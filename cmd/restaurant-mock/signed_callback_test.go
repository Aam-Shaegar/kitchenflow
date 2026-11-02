package main

import (
	"encoding/base64"
	"encoding/json"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/callbackauth"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSignedCallbackRetryThroughVerifier(t *testing.T) {
	rid := uuid.New()
	raw, _ := json.Marshal([]map[string]any{{"key_id": "active", "restaurant_id": rid, "secret_base64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32))), "expires_at": time.Now().Add(time.Hour)}})
	file := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	keys, err := callbackauth.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan StatusCallbackRequest, 4)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/integrations/restaurants/{restaurant_id}/orders/status", keys.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q StatusCallbackRequest
		_ = json.NewDecoder(r.Body).Decode(&q)
		events <- q
		if len(events) == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	})))
	server := httptest.NewServer(mux)
	defer server.Close()
	store := NewMockStore()
	defer store.Close()
	store.signCallback, err = keys.Signer("active", rid)
	if err != nil {
		t.Fatal(err)
	}
	store.QueueCallback(server.URL, rid.String(), "order", "confirmed")
	// Keep the first event in the channel until both attempts arrive.
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for len(events) < 2 {
		select {
		case <-deadline.C:
			t.Fatal("signed retry not delivered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	store.Close()
	first, second := <-events, <-events
	if first != second || first.EventID == "" {
		t.Fatal("signed retry changed event")
	}
}
