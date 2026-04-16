package approval

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestLocalDesktopClientSubmitAndGetStatus(t *testing.T) {
	notifyCalled := false
	askCalled := false
	bypassAskCalled := false
	client := NewLocalDesktopClient(LocalDesktopClientConfig{
		RequestTimeout: 500 * time.Millisecond,
		Notifier: func(title, message string) error {
			notifyCalled = true
			if title == "" || message == "" {
				t.Fatalf("empty notification title/message")
			}
			return nil
		},
		Asker: func(title, message string) (bool, string, error) {
			askCalled = true
			return true, "ok", nil
		},
		BypassAsker: func(title, message string, defaultTTL time.Duration) (BypassScope, time.Duration, error) {
			bypassAskCalled = true
			return BypassScopeTable, defaultTTL, nil
		},
		DefaultBypassTTL: 45 * time.Minute,
	})

	extID, err := client.Submit(context.Background(), SubmitRequest{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "users",
		Summary:   "tool=query_table table=users",
	})
	if err != nil {
		t.Fatalf("Submit err=%v", err)
	}
	if extID == "" {
		t.Fatal("empty external id")
	}

	result := waitFinalStatus(t, client, extID, 500*time.Millisecond)
	if result.Status != StatusApproved || result.Reason != "ok" {
		t.Fatalf("result=%+v", result)
	}
	if result.BypassScope != BypassScopeTable || result.BypassTTL != 45*time.Minute {
		t.Fatalf("unexpected bypass policy: %+v", result)
	}
	if !notifyCalled || !askCalled || !bypassAskCalled {
		t.Fatalf("notifyCalled=%v askCalled=%v bypassAskCalled=%v", notifyCalled, askCalled, bypassAskCalled)
	}
}

func TestLocalDesktopClientTimeout(t *testing.T) {
	client := NewLocalDesktopClient(LocalDesktopClientConfig{
		RequestTimeout: 20 * time.Millisecond,
		Asker: func(title, message string) (bool, string, error) {
			time.Sleep(80 * time.Millisecond)
			return true, "late", nil
		},
	})
	extID, err := client.Submit(context.Background(), SubmitRequest{RequestID: "req-2"})
	if err != nil {
		t.Fatalf("Submit err=%v", err)
	}

	result := waitFinalStatus(t, client, extID, 500*time.Millisecond)
	if result.Status != StatusRejected {
		t.Fatalf("status=%s", result.Status)
	}
	if result.Reason == "" {
		t.Fatal("expected timeout reason")
	}
}

func TestLocalDesktopClientAskerError(t *testing.T) {
	client := NewLocalDesktopClient(LocalDesktopClientConfig{
		RequestTimeout: 200 * time.Millisecond,
		Asker: func(title, message string) (bool, string, error) {
			return false, "", errors.New("ask failed")
		},
	})
	extID, err := client.Submit(context.Background(), SubmitRequest{RequestID: "req-3"})
	if err != nil {
		t.Fatalf("Submit err=%v", err)
	}

	result := waitFinalStatus(t, client, extID, 500*time.Millisecond)
	if result.Status != StatusRejected {
		t.Fatalf("status=%s", result.Status)
	}
	if result.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestLocalDesktopClientGetStatusUnknownIDRePrompts(t *testing.T) {
	askCalled := false
	client := NewLocalDesktopClient(LocalDesktopClientConfig{
		RequestTimeout: 500 * time.Millisecond,
		Notifier: func(title, message string) error {
			return nil
		},
		Asker: func(title, message string) (bool, string, error) {
			askCalled = true
			return true, "re-approved", nil
		},
		BypassAsker: func(title, message string, defaultTTL time.Duration) (BypassScope, time.Duration, error) {
			return BypassScopeOneTime, 0, nil
		},
	})

	st, err := client.GetStatus(context.Background(), "lost-external-id")
	if err != nil {
		t.Fatalf("GetStatus err=%v", err)
	}
	if st.Status != StatusPending {
		t.Fatalf("status=%s", st.Status)
	}

	result := waitFinalStatus(t, client, "lost-external-id", 500*time.Millisecond)
	if result.Status != StatusApproved || result.Reason != "re-approved" {
		t.Fatalf("result=%+v", result)
	}
	if !askCalled {
		t.Fatal("expected asker to be called")
	}
}

func waitFinalStatus(t *testing.T, client *LocalDesktopClient, externalID string, maxWait time.Duration) StatusResult {
	t.Helper()
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		result, err := client.GetStatus(context.Background(), externalID)
		if err != nil {
			t.Fatalf("GetStatus err=%v", err)
		}
		if result.Status != StatusPending {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status still pending after %s", maxWait)
	return StatusResult{}
}

func TestDefaultDesktopAskerUnsupportedOS(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		t.Skip("would open real desktop prompt on this OS")
	}
	ok, reason, err := defaultDesktopAsker("title", "message")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if ok {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	if reason == "" {
		t.Fatalf("reason=%q", reason)
	}
}
