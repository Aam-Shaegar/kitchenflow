package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/callbackauth"
	request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	serverbounds "github.com/Aam-Shaegar/kitchenflow/internal/core/http/server"
	"github.com/google/uuid"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	ordersdomain "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
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
	signCallback      func(*http.Request, []byte) error
	ctx               context.Context
	cancel            context.CancelFunc
	tasksMu           sync.Mutex
	tasks             sync.WaitGroup
	closed            bool
	events            []callbackEvent
	wake              chan struct{}
	dispatcherStarted bool
	mu                sync.RWMutex
	orders            map[string]*StoredOrder
}

func NewMockStore() *MockStore {
	ctx, cancel := context.WithCancel(context.Background())
	return &MockStore{orders: map[string]*StoredOrder{}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
}

// CreateOrGet is the only creation path. The returned flag alone grants the
// caller permission to start processing or emit the initial rejection callback.
func (s *MockStore) CreateOrGet(req CreateOrderRequest, status string, available bool) (*StoredOrder, bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.orders[req.ExternalOrderID]; ok {
		if previous.DeliveryAddress != req.DeliveryAddress || !slices.Equal(previous.Items, req.Items) {
			return nil, false, http.StatusConflict
		}
		return cloneOrder(previous), false, 0
	}
	if !available {
		return nil, false, http.StatusServiceUnavailable
	}
	now := time.Now()
	o := &StoredOrder{ExternalOrderID: req.ExternalOrderID, Items: append([]CreateOrderItem(nil), req.Items...), DeliveryAddress: req.DeliveryAddress, Status: status, CreatedAt: now, UpdatedAt: now}
	s.orders[o.ExternalOrderID] = o
	return cloneOrder(o), true, 0
}
func cloneOrder(o *StoredOrder) *StoredOrder {
	copy := *o
	copy.Items = append([]CreateOrderItem(nil), o.Items...)
	return &copy
}

func (s *MockStore) Get(id string) (*StoredOrder, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[id]
	if !ok {
		return nil, false
	}
	return cloneOrder(o), true
}

func (s *MockStore) List() []*StoredOrder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*StoredOrder, 0, len(s.orders))
	for _, o := range s.orders {
		out = append(out, cloneOrder(o))
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

func configFromEnv() (MockConfig, error) {
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
			} else if n, err := strconv.Atoi(p); err == nil && n >= 0 && n <= 86400 {
				parsed = append(parsed, time.Duration(n)*time.Second)
			} else {
				return MockConfig{}, fmt.Errorf("invalid MOCK_DELAYS")
			}
		}
		if len(parsed) > 0 {
			delays = parsed
		}
	}
	if mode != "auto" && mode != "manual" && mode != "rejecting" && mode != "failing" {
		return MockConfig{}, fmt.Errorf("invalid MOCK_MODE")
	}
	for _, delay := range delays {
		if delay < 0 {
			return MockConfig{}, fmt.Errorf("negative MOCK_DELAYS")
		}
	}
	return MockConfig{Mode: mode, ProcessingDelays: delays}, nil
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

	endpoint, err := url.Parse(callbackURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		log.Fatal("invalid KITCHEN_CALLBACK_URL")
	}
	restaurantID := "550e8400-e29b-41d4-a716-446655440000"
	if envID := os.Getenv("RESTAURANT_ID"); envID != "" {
		restaurantID = envID
	}

	keys, err := callbackauth.Load(os.Getenv("CALLBACK_KEYS_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	rid, err := uuid.Parse(restaurantID)
	if err != nil {
		log.Fatal("invalid RESTAURANT_ID")
	}
	signer, err := keys.Signer(os.Getenv("CALLBACK_KEY_ID"), rid)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := configFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		log.Fatal("invalid PORT")
	}
	store := NewMockStore()
	store.signCallback = signer
	defer store.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/restaurant/orders", handleOrders(store, callbackURL, restaurantID, cfg))
	mux.HandleFunc("/api/v1/restaurant/orders/", handleOrderByID(store, callbackURL, restaurantID))
	mux.HandleFunc("/api/v1/restaurant/menu", handleMenu)
	mux.HandleFunc("/health", handleHealth)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           withLogging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverbounds.ApplyBounds(server)
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

func handleOrderByID(store *MockStore, callbackURL, restaurantID string) http.HandlerFunc {
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
			if err := request.DecodeObject(r, &req); err != nil {
				http.Error(w, "invalid request", request.JSONErrorStatus(err))
				return
			}
			if code := store.NotifyStatus(callbackURL, restaurantID, id, req.Status); code != 0 {
				http.Error(w, http.StatusText(code), code)
				return
			}
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
		if err := request.DecodeObject(r, &req); err != nil {
			http.Error(w, "invalid request", request.JSONErrorStatus(err))
			return
		}
		if strings.TrimSpace(req.DeliveryAddress) == "" {
			http.Error(w, "delivery_address required", http.StatusBadRequest)
			return
		}

		if strings.TrimSpace(req.ExternalOrderID) == "" || len(req.ExternalOrderID) > 128 {
			http.Error(w, "external_order_id required (max 128)", http.StatusBadRequest)
			return
		}
		status := "confirmed"
		if cfg.Mode == "rejecting" || strings.Contains(strings.ToLower(req.DeliveryAddress), "reject") {
			status = "cancelled"
		}
		order, created, code := store.CreateOrGet(req, status, cfg.Mode != "failing")
		if code != 0 {
			http.Error(w, http.StatusText(code), code)
			return
		}
		// Dispatch side effects before writing: a lost HTTP response must not
		// prevent the one newly accepted order from starting processing.
		if created {
			if status == "cancelled" {
				store.QueueCallback(callbackURL, restaurantID, order.ExternalOrderID, status)
			} else if cfg.Mode != "manual" {
				store.start(func() { simulateOrderProcessing(store, callbackURL, restaurantID, order.ExternalOrderID, cfg) })
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if created {
			w.WriteHeader(http.StatusCreated)
		}
		if err := json.NewEncoder(w).Encode(CreateOrderResponse{ExternalOrderID: order.ExternalOrderID, Status: order.Status}); err != nil {
			log.Printf("Failed to encode response: %v", err)
		}
	}
}

func simulateOrderProcessing(store *MockStore, callbackURL, restaurantID, externalOrderID string, cfg MockConfig) {
	statuses := []string{"confirmed", "preparing", "ready", "delivering", "delivered"}
	for i, status := range statuses {
		delay := 2 * time.Second
		if i < len(cfg.ProcessingDelays) {
			delay = cfg.ProcessingDelays[i]
		}
		if !waitFor(store.ctx, delay) {
			return
		}
		if _, ok := store.Get(externalOrderID); !ok {
			return
		}
		if store.NotifyStatus(callbackURL, restaurantID, externalOrderID, status) != 0 {
			return
		}
	}
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
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ExternalOrderID string `json:"external_order_id"`
		}
		if err := request.DecodeObject(r, &req); err != nil {
			http.Error(w, "invalid request", request.JSONErrorStatus(err))
			return
		}
		if req.ExternalOrderID == "" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		o, ok := store.orders[req.ExternalOrderID]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if o.Status != "cancelled" && !ordersdomain.CanTransition(ordersdomain.OrderStatus(o.Status), ordersdomain.OrderStatusCancelled) {
			http.Error(w, "already cooking", http.StatusConflict)
			return
		}
		o.Status = "cancelled"
		o.UpdatedAt = time.Now()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CreateOrderResponse{ExternalOrderID: o.ExternalOrderID, Status: o.Status})
	}
}
