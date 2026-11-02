package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCallbackRetriesPreserveEventAndOrder(t *testing.T) {
	var mu sync.Mutex
	var attempts []StatusCallbackRequest
	delivered := make(chan StatusCallbackRequest, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q StatusCallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
		}
		mu.Lock()
		attempts = append(attempts, q)
		n := len(attempts)
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(503)
			return
		}
		if n == 2 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		delivered <- q
		w.WriteHeader(204)
	}))
	defer server.Close()
	store := NewMockStore()
	defer store.Close()
	store.CreateOrGet(CreateOrderRequest{ExternalOrderID: "order"}, "confirmed", true)
	if code := store.NotifyStatus(server.URL, "restaurant", "order", "preparing"); code != 0 {
		t.Fatal(code)
	}
	if code := store.NotifyStatus(server.URL, "restaurant", "order", "ready"); code != 0 {
		t.Fatal(code)
	}
	for _, status := range []string{"preparing", "ready"} {
		select {
		case q := <-delivered:
			if q.Status != status {
				t.Fatalf("event order %s want %s", q.Status, status)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("callback not recovered")
		}
	}
	store.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 4 {
		t.Fatalf("attempts=%d", len(attempts))
	}
	for i := 1; i < 3; i++ {
		if attempts[i] != attempts[0] {
			t.Fatal("retry changed event payload or ID")
		}
	}
	if attempts[3].EventID == attempts[0].EventID {
		t.Fatal("different events share ID")
	}
}
func TestShutdownStopsProcessingAndRetryWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	store := NewMockStore()
	store.CreateOrGet(CreateOrderRequest{ExternalOrderID: "order"}, "confirmed", true)
	store.QueueCallback(server.URL, "restaurant", "order", "confirmed")
	store.start(func() {
		simulateOrderProcessing(store, server.URL, "restaurant", "order", MockConfig{ProcessingDelays: []time.Duration{time.Hour}})
	})
	done := make(chan struct{})
	go func() { store.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked on sleep/retry")
	}
	o, _ := store.Get("order")
	if o.Status != "confirmed" {
		t.Fatal("processing advanced after shutdown")
	}
}
func TestManualStatusCodes(t *testing.T) {
	store := NewMockStore()
	defer store.Close()
	store.CreateOrGet(CreateOrderRequest{ExternalOrderID: "order"}, "confirmed", true)
	handler := handleOrderByID(store, "http://127.0.0.1:1", "restaurant", MockConfig{Mode: "manual"})
	for _, tc := range []struct {
		id, status string
		code       int
	}{{"missing", "preparing", 404}, {"order", "unknown", 400}, {"order", "delivered", 409}, {"order", "preparing", 204}, {"order", "cancelled", 409}} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/api/v1/restaurant/orders/"+tc.id+"/status", strings.NewReader(`{"status":"`+tc.status+`"}`)))
		if w.Code != tc.code {
			t.Fatalf("%s: HTTP %d want %d", tc.status, w.Code, tc.code)
		}
	}
}

func TestShutdownCancelsInFlightCallback(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q StatusCallbackRequest
		_ = json.NewDecoder(r.Body).Decode(&q)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	store := NewMockStore()
	defer store.Close()
	store.QueueCallback(server.URL, "restaurant", "order", "confirmed")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	done := make(chan struct{})
	go func() { store.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight HTTP not cancelled")
	}
}
