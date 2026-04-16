package approval

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestApprovalClientSubmitAndGetStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/approvals", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"approval_id": "ext-1",
		})
	})
	mux.HandleFunc("/approvals/ext-1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "approved",
			"reason":       "ok",
			"bypass_scope": "table",
			"bypass_ttl":   "45m",
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cli := NewClient(ClientConfig{
		BaseURL:            ts.URL,
		SubmitPath:         "/approvals",
		StatusPathTemplate: "/approvals/{id}",
		HTTPClient:         ts.Client(),
	})

	externalID, err := cli.Submit(context.Background(), SubmitRequest{RequestID: "req-1", Fingerprint: "fp"})
	if err != nil {
		t.Fatalf("Submit err=%v", err)
	}
	if externalID != "ext-1" {
		t.Fatalf("externalID=%q", externalID)
	}

	result, err := cli.GetStatus(context.Background(), "ext-1")
	if err != nil {
		t.Fatalf("GetStatus err=%v", err)
	}
	if result.Status != StatusApproved || result.Reason != "ok" {
		t.Fatalf("result=%+v", result)
	}
	if result.BypassScope != BypassScopeTable || result.BypassTTL != 45*time.Minute {
		t.Fatalf("unexpected bypass policy: %+v", result)
	}
}

func TestVerifyCallbackSignature(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"approval_id":"a1","status":"approved"}`)
	tsValue := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(tsValue))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	ok := VerifyCallbackSignature(secret, tsValue, sig, body, 5*time.Minute)
	if !ok {
		t.Fatal("expected signature valid")
	}

	ok = VerifyCallbackSignature(secret, "1", sig, body, 5*time.Minute)
	if ok {
		t.Fatal("expected signature invalid for stale timestamp")
	}
}
