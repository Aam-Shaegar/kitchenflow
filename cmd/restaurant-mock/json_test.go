package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMalformedBodiesNeverChangeMockState(t *testing.T) {
	store := NewMockStore()
	defer store.Close()
	for _, body := range []string{`{"external_order_id":"order","delivery_address":"Address"} {}`, `{"external_order_id":"order","delivery_address":"Address"} garbage`} {
		w := httptest.NewRecorder()
		handleCreateOrder(store, "", "", MockConfig{Mode: "manual"})(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		if len(store.List()) != 0 {
			t.Fatal("malformed create changed state")
		}
	}
	store.CreateOrGet(CreateOrderRequest{ExternalOrderID: "order"}, "confirmed", true)
	for _, endpoint := range []string{"cancel", "order/status"} {
		body := `{"external_order_id":"order"} {}`
		if endpoint == "order/status" {
			body = `{"status":"preparing"} {}`
		}
		w := httptest.NewRecorder()
		handleOrderByID(store, "", "")(w, httptest.NewRequest("POST", "/api/v1/restaurant/orders/"+endpoint, strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		o, _ := store.Get("order")
		if o.Status != "confirmed" {
			t.Fatal("malformed mutation changed state")
		}
	}
}
