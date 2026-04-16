package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/xiluoduyu/mysql-mcp/internal/approval"
	"github.com/xiluoduyu/mysql-mcp/internal/config"
	"github.com/xiluoduyu/mysql-mcp/internal/mcpserver"
	"github.com/xiluoduyu/mysql-mcp/internal/mysqlquery"
)

func openMySQLSources(ctx context.Context, cfg config.Config) (map[string]*sql.DB, error) {
	sources := map[string]*sql.DB{}
	closeAll := func() {
		for _, db := range sources {
			_ = db.Close()
		}
	}
	for _, entry := range cfg.MySQLDSNs {
		db, err := sql.Open("mysql", entry.DSN)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("open mysql source %s error: %w", entry.Name, err)
		}
		sources[entry.Name] = db
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no mysql sources configured")
	}
	for name, db := range sources {
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := db.PingContext(pingCtx)
		cancel()
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("ping mysql source %s error: %w", name, err)
		}
	}
	return sources, nil
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("get current working directory error: %v", err)
	}
	cmd := buildCommand(cwd, os.Stdout, os.Stderr)
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatalf("run command error: %v", err)
	}
}

func runServe(ctx context.Context, opts *appOptions, cwd string) error {
	loadCfg, err := loadRuntimeConfig(opts, cwd)
	if err != nil {
		return err
	}

	mysqlSources, err := openMySQLSources(ctx, loadCfg)
	if err != nil {
		return fmt.Errorf("init mysql sources error: %w", err)
	}
	defer func() {
		for _, db := range mysqlSources {
			_ = db.Close()
		}
	}()
	if _, ok := mysqlSources[mysqlquery.DefaultSource]; !ok {
		for name := range mysqlSources {
			log.Printf("mysql default source not configured; callers must pass source explicitly (available: %s)", name)
			break
		}
	}

	querySvc, err := mcpserver.NewMySQLService(
		ctx,
		mysqlSources,
		loadCfg.MaxLimit,
		mysqlquery.WithMasking(loadCfg.MaskFieldKeywords, loadCfg.MaskFields, loadCfg.MaskJSONFields),
	)
	if err != nil {
		return fmt.Errorf("new mysql query service error: %w", err)
	}

	store, err := approval.NewSQLiteStore(loadCfg.StateSQLitePath)
	if err != nil {
		return fmt.Errorf("new sqlite store error: %w", err)
	}
	defer store.Close()

	var approvalClient approval.ApprovalClient
	switch loadCfg.ApprovalClientMode {
	case "local_desktop":
		approvalClient = approval.NewLocalDesktopClient(approval.LocalDesktopClientConfig{
			RequestTimeout: loadCfg.ApprovalTimeout,
		})
	default:
		approvalClient = approval.NewClient(approval.ClientConfig{
			BaseURL:            loadCfg.ApprovalBaseURL,
			SubmitPath:         loadCfg.ApprovalSubmitPath,
			StatusPathTemplate: loadCfg.ApprovalStatusPathTemplate,
		})
	}
	gate := approval.NewGate(store, approvalClient, loadCfg.ApprovalTimeout)

	srv := mcpserver.New(mcpserver.Config{
		BearerToken:        loadCfg.BearerToken,
		CallbackSecret:     loadCfg.ApprovalCallbackSecret,
		ApprovalBypassTTL:  loadCfg.ApprovalTimeout,
		ApprovalPollPeriod: loadCfg.ApprovalPollInterval,
	}, querySvc, gate, store)

	runCtx, runCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer runCancel()

	log.Printf("mysql mcp server listening on %s", loadCfg.BindAddr)
	if err := srv.Start(runCtx, loadCfg.BindAddr); err != nil && err != context.Canceled {
		return fmt.Errorf("server stopped with error: %w", err)
	}
	fmt.Println("server stopped")
	return nil
}
