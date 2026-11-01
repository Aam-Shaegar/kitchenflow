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

func TestConcurrentCreateRunsOneProcessingSequence(t *testing.T) {
	events := make(chan string, 200)
	callbacks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q StatusCallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
		}
		events <- q.Status
		w.WriteHeader(204)
	}))
	defer callbacks.Close()
	store := NewMockStore()
	handler := handleCreateOrder(store, callbacks.URL, "restaurant", MockConfig{ProcessingDelays: []time.Duration{0, 0, 0, 0, 0}})
	payload := `{"external_order_id":"same","delivery_address":"Test address","items":[{"external_menu_item_id":"dish","name":"Dish","unit_price":100,"quantity":1}]}`
	var wg sync.WaitGroup
	codes := make(chan int, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest("POST", "/", strings.NewReader(payload)))
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		if code == 201 {
			created++
		} else if code != 200 {
			t.Fatalf("HTTP %d", code)
		}
	}
	if created != 1 {
		t.Fatalf("created=%d", created)
	}
	statuses := []string{"confirmed", "preparing", "ready", "delivering", "delivered"}
	for _, want := range statuses {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("callback %s want %s", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("processing did not finish")
		}
	}
	select {
	case status := <-events:
		t.Fatalf("duplicate processing callback %s", status)
	case <-time.After(100 * time.Millisecond):
	}
	before, _ := store.Get("same")
	for _, changed := range []string{strings.Replace(payload, "Test address", "Other address", 1), strings.Replace(payload, `"quantity":1`, `"quantity":2`, 1), strings.Replace(payload, `"unit_price":100`, `"unit_price":200`, 1), strings.Replace(payload, `"name":"Dish"`, `"name":"Other"`, 1)} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/", strings.NewReader(changed)))
		if w.Code != 409 {
			t.Fatalf("payload mismatch HTTP %d", w.Code)
		}
	}
	after, _ := store.Get("same")
	if after.Status != "delivered" || !after.CreatedAt.Equal(before.CreatedAt) || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("replays modified stored order")
	}
}
func TestCompetingPayloadsAndStoreOwnership(t *testing.T) {
	store := NewMockStore()
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	handler := handleCreateOrder(store, "", "", MockConfig{Mode: "manual"})
	for _, address := range []string{"Address A", "Address B"} {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"external_order_id":"same","delivery_address":"`+address+`"}`)))
			codes <- w.Code
		}(address)
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	for _, id := range []string{"", "   "} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"external_order_id":"`+id+`","delivery_address":"Address"}`)))
		if w.Code != 400 {
			t.Fatalf("missing ID HTTP %d", w.Code)
		}
	}
	req := CreateOrderRequest{ExternalOrderID: "owned", Items: []CreateOrderItem{{Name: "Original"}}}
	result, _, _ := store.CreateOrGet(req, "confirmed", true)
	req.Items[0].Name = "Changed"
	result.Items[0].Name = "Also changed"
	listed := store.List()
	for _, o := range listed {
		o.Status = "cancelled"
	}
	saved, _ := store.Get("owned")
	if saved.Items[0].Name != "Original" || saved.Status != "confirmed" {
		t.Fatal("external mutation changed stored order")
	}
}

func TestKnownOrderReplaysDuringOutage(t *testing.T) {
	for _, status := range []string{"confirmed", "cancelled", "delivered"} {
		store := NewMockStore()
		req := CreateOrderRequest{ExternalOrderID: "known", DeliveryAddress: "Address"}
		store.CreateOrGet(req, status, true)
		handler := handleCreateOrder(store, "", "", MockConfig{Mode: "failing"})
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"external_order_id":"known","delivery_address":"Address"}`)))
		if w.Code != 200 {
			t.Fatalf("replay %s HTTP %d", status, w.Code)
		}
		var result CreateOrderResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != status {
			t.Fatal("saved status changed")
		}
		w = httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"external_order_id":"new","delivery_address":"Address"}`)))
		if w.Code != 503 {
			t.Fatal("outage accepted new order")
		}
		if _, ok := store.Get("new"); ok {
			t.Fatal("outage persisted new order")
		}
	}
}
