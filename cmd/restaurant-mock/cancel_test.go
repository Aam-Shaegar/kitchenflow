package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCancellationRacesWithCooking(t *testing.T) {
	for i := 0; i < 100; i++ {
		store := NewMockStore()
		store.Put(&StoredOrder{ExternalOrderID: "order", Status: "confirmed"})
		var wg sync.WaitGroup
		wg.Add(2)
		var cooked bool
		var code int
		go func() { defer wg.Done(); cooked = store.SetStatus("order", "preparing") }()
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			handleCancel(store)(w, httptest.NewRequest("POST", "/api/v1/restaurant/orders/cancel", strings.NewReader(`{"external_order_id":"order"}`)))
			code = w.Code
		}()
		wg.Wait()
		o, _ := store.Get("order")
		if cooked {
			if code != 409 || o.Status != "preparing" {
				t.Fatalf("cooking outcome %d %s", code, o.Status)
			}
		} else {
			if code != 200 || o.Status != "cancelled" {
				t.Fatalf("cancel outcome %d %s", code, o.Status)
			}
			if store.SetStatus("order", "preparing") {
				t.Fatal("cancelled order resumed")
			}
		}
	}
}
func TestCancelRepeatAndAutoProcessingStops(t *testing.T) {
	store := NewMockStore()
	store.Put(&StoredOrder{ExternalOrderID: "order", Status: "pending"})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		handleCancel(store)(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"external_order_id":"order"}`)))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	simulateOrderProcessing(store, "", "", "order", MockConfig{ProcessingDelays: []time.Duration{0}})
	o, _ := store.Get("order")
	if o.Status != "cancelled" {
		t.Fatal(o.Status)
	}
}
