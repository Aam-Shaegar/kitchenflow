package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	ordersdomain "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
)

type CreateOrderRequest struct {
	ExternalOrderID string            `json:"external_order_id"`
	Items           []CreateOrderItem `json:"items"`
	DeliveryAddress string            `json:"delivery_address"`
}

type CreateOrderItem struct {
	ExternalMenuItemID string `json:"external_menu_item_id"`
	Name               string `json:"name"`
	UnitPrice          int64  `json:"unit_price"`
	Quantity           int    `json:"quantity"`
}

type CreateOrderResponse struct {
	ExternalOrderID string `json:"external_order_id"`
	Status          string `json:"status"`
}

type StatusCallbackRequest struct {
	ExternalOrderID string `json:"external_order_id"`
	Status          string `json:"status"`
	EventID         string `json:"event_id"`
}

type StoredOrder struct {
	ExternalOrderID string            `json:"external_order_id"`
	Items           []CreateOrderItem `json:"items"`
	DeliveryAddress string            `json:"delivery_address"`
	Status          string            `json:"status"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type MockStore struct {
	createMu sync.Mutex
	mu       sync.RWMutex
	orders   map[string]*StoredOrder
}

func NewMockStore() *MockStore {
	return &MockStore{orders: map[string]*StoredOrder{}}
}

func (s *MockStore) Put(o *StoredOrder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders[o.ExternalOrderID] = o
}

func (s *MockStore) Get(id string) (*StoredOrder, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[id]
	if !ok {
		return nil, false
	}
	copy := *o
	copy.Items = append([]CreateOrderItem(nil), o.Items...)
	return &copy, true
}

func (s *MockStore) List() []*StoredOrder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*StoredOrder, 0, len(s.orders))
	for _, o := range s.orders {
		out = append(out, o)
	}
	return out
}

func (s *MockStore) SetStatus(id, status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok {
		return false
	}
	if o.Status != status && !ordersdomain.CanTransition(ordersdomain.OrderStatus(o.Status), ordersdomain.OrderStatus(status)) {
		return false
	}
	o.Status = status
	o.UpdatedAt = time.Now()
	return ok
}

type MockConfig struct {
	// Mode selects the kitchen scenario: auto, manual, rejecting or failing.
	Mode             string
	ProcessingDelays []time.Duration
}

func configFromEnv() MockConfig {
	mode := strings.ToLower(os.Getenv("MOCK_MODE"))
	if mode == "" {
		mode = "auto"
	}
	delays := []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second, 2 * time.Second}
	if raw := os.Getenv("MOCK_DELAYS"); raw != "" {
		parts := strings.Split(raw, ",")
		var parsed []time.Duration
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if d, err := time.ParseDuration(p); err == nil {
				parsed = append(parsed, d)
			} else if n, err := strconv.Atoi(p); err == nil {
				parsed = append(parsed, time.Duration(n)*time.Second)
			}
		}
		if len(parsed) > 0 {
			delays = parsed
		}
	}
	return MockConfig{Mode: mode, ProcessingDelays: delays}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	callbackURL := os.Getenv("KITCHEN_CALLBACK_URL")
	if callbackURL == "" {
		callbackURL = "http://kitchen-api:8080"
	}

	restaurantID := "550e8400-e29b-41d4-a716-446655440000"
	if envID := os.Getenv("RESTAURANT_ID"); envID != "" {
		restaurantID = envID
	}

	cfg := configFromEnv()
	store := NewMockStore()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/restaurant/orders", handleOrders(store, callbackURL, restaurantID, cfg))
	mux.HandleFunc("/api/v1/restaurant/orders/", handleOrderByID(store, callbackURL, restaurantID, cfg))
	mux.HandleFunc("/api/v1/restaurant/menu", handleMenu)
	mux.HandleFunc("/health", handleHealth)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           withLogging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("Restaurant mock starting on port %s mode=%s restaurant=%s", port, cfg.Mode, restaurantID)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func handleOrders(store *MockStore, callbackURL, restaurantID string, cfg MockConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateOrder(store, callbackURL, restaurantID, cfg)(w, r)
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(store.List())
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleOrderByID(store *MockStore, callbackURL, restaurantID string, cfg MockConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The same prefix serves both the order card and the manual status trigger.
		if r.URL.Path == "/api/v1/restaurant/orders/cancel" {
			handleCancel(store)(w, r)
			return
		}
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/restaurant/orders/")
		parts := strings.Split(trimmed, "/")
		id := parts[0]
		if id == "" {
			http.Error(w, "missing order id", http.StatusBadRequest)
			return
		}
		if len(parts) == 2 && parts[1] == "status" && r.Method == http.MethodPost {
			var req struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			if !store.SetStatus(id, req.Status) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			go sendCallback(callbackURL, restaurantID, id, req.Status)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		order, ok := store.Get(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(order)
	}
}

func handleCreateOrder(store *MockStore, callbackURL, restaurantID string, cfg MockConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.DeliveryAddress) == "" {
			http.Error(w, "delivery_address required", http.StatusBadRequest)
			return
		}

		store.createMu.Lock()
		defer store.createMu.Unlock()
		if previous, ok := store.Get(req.ExternalOrderID); ok {
			if previous.DeliveryAddress != req.DeliveryAddress || !slices.Equal(previous.Items, req.Items) {
				http.Error(w, "order ID payload conflict", 409)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(CreateOrderResponse{ExternalOrderID: previous.ExternalOrderID, Status: previous.Status})
			return
		}
		// Failing mode lets us rehearse outages: every create attempt gets a 503.
		if cfg.Mode == "failing" {
			http.Error(w, "restaurant temporarily unavailable", http.StatusServiceUnavailable)
			return
		}

		externalOrderID := req.ExternalOrderID
		if externalOrderID == "" {
			externalOrderID = uuid.New().String()
		}

		// A rejection still starts with 201 so Kitchen learns the external id;
		// the following cancelled callback then walks the legal pending -> cancelled edge.
		if cfg.Mode == "rejecting" || strings.Contains(strings.ToLower(req.DeliveryAddress), "reject") {
			store.Put(&StoredOrder{
				ExternalOrderID: externalOrderID,
				Items:           req.Items,
				DeliveryAddress: req.DeliveryAddress,
				Status:          "cancelled",
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			})
			go sendCallback(callbackURL, restaurantID, externalOrderID, "cancelled")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(CreateOrderResponse{
				ExternalOrderID: externalOrderID,
				Status:          "cancelled",
			})
			return
		}

		store.Put(&StoredOrder{
			ExternalOrderID: externalOrderID,
			Items:           req.Items,
			DeliveryAddress: req.DeliveryAddress,
			Status:          "confirmed",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		})

		resp := CreateOrderResponse{
			ExternalOrderID: externalOrderID,
			Status:          "confirmed",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Failed to encode response: %v", err)
		}

		if cfg.Mode == "manual" {
			log.Printf("Manual mode: order %s waits for explicit status trigger", externalOrderID)
			return
		}
		go simulateOrderProcessing(store, callbackURL, restaurantID, externalOrderID, cfg)
	}
}

func simulateOrderProcessing(store *MockStore, callbackURL, restaurantID, externalOrderID string, cfg MockConfig) {
	statuses := []string{"confirmed", "preparing", "ready", "delivering", "delivered"}
	for i, status := range statuses {
		delay := 2 * time.Second
		if i < len(cfg.ProcessingDelays) {
			delay = cfg.ProcessingDelays[i]
		}
		time.Sleep(delay)
		if _, ok := store.Get(externalOrderID); !ok {
			return
		}
		if !store.SetStatus(externalOrderID, status) {
			return
		}
		sendCallback(callbackURL, restaurantID, externalOrderID, status)
	}
}

func sendCallback(callbackURL, restaurantID, externalOrderID, status string) {
	payload := StatusCallbackRequest{
		ExternalOrderID: externalOrderID,
		Status:          status,
		EventID:         uuid.New().String(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal callback: %v", err)
		return
	}

	url := fmt.Sprintf("%s/api/v1/integrations/restaurants/%s/orders/status", callbackURL, restaurantID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to create callback request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Callback failed: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("Callback sent: order=%s status=%s response=%d", externalOrderID, status, resp.StatusCode)
}

func handleMenu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"note": "demo menu is owned by Kitchen API; this endpoint shows the mock contract",
		"categories": []map[string]any{
			{"name": "Burgers", "items": 3},
			{"name": "Drinks", "items": 2},
			{"name": "Sides", "items": 2},
		},
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		log.Printf("Failed to write health response: %v", err)
	}
}

func handleCancel(store *MockStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var req struct {
			ExternalOrderID string `json:"external_order_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExternalOrderID == "" {
			http.Error(w, "invalid request", 400)
			return
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		o, ok := store.orders[req.ExternalOrderID]
		if !ok {
			http.Error(w, "not found", 404)
			return
		}
		if o.Status != "cancelled" && !ordersdomain.CanTransition(ordersdomain.OrderStatus(o.Status), ordersdomain.OrderStatusCancelled) {
			http.Error(w, "already cooking", 409)
			return
		}
		o.Status = "cancelled"
		o.UpdatedAt = time.Now()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CreateOrderResponse{ExternalOrderID: o.ExternalOrderID, Status: o.Status})
	}
}
