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
	if err := config.LoadDotEnvFile(config.DefaultDotEnvPath); err != nil {
		log.Fatalf("load .env error: %v", err)
	}

	cfg, err := config.LoadFromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("load config error: %v", err)
	}

	ctx := context.Background()
	mysqlSources, err := openMySQLSources(ctx, cfg)
	if err != nil {
		log.Fatalf("init mysql sources error: %v", err)
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
		cfg.MaxLimit,
		mysqlquery.WithMasking(cfg.MaskFieldKeywords, cfg.MaskFields, cfg.MaskJSONFields),
	)
	if err != nil {
		log.Fatalf("new mysql query service error: %v", err)
	}

	store, err := approval.NewSQLiteStore(cfg.StateSQLitePath)
	if err != nil {
		log.Fatalf("new sqlite store error: %v", err)
	}
	defer store.Close()

	var approvalClient approval.ApprovalClient
	switch cfg.ApprovalClientMode {
	case "local_desktop":
		approvalClient = approval.NewLocalDesktopClient(approval.LocalDesktopClientConfig{
			RequestTimeout: cfg.ApprovalTimeout,
		})
	default:
		approvalClient = approval.NewClient(approval.ClientConfig{
			BaseURL:            cfg.ApprovalBaseURL,
			SubmitPath:         cfg.ApprovalSubmitPath,
			StatusPathTemplate: cfg.ApprovalStatusPathTemplate,
		})
	}
	gate := approval.NewGate(store, approvalClient, cfg.ApprovalTimeout)

	srv := mcpserver.New(mcpserver.Config{
		BearerToken:        cfg.BearerToken,
		CallbackSecret:     cfg.ApprovalCallbackSecret,
		ApprovalBypassTTL:  cfg.ApprovalTimeout,
		ApprovalPollPeriod: cfg.ApprovalPollInterval,
	}, querySvc, gate, store)

	runCtx, runCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer runCancel()

	log.Printf("mysql mcp server listening on %s", cfg.BindAddr)
	if err := srv.Start(runCtx, cfg.BindAddr); err != nil && err != context.Canceled {
		log.Fatalf("server stopped with error: %v", err)
	}

	fmt.Println("server stopped")
}
