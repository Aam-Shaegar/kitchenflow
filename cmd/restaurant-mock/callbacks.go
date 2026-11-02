package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
	"io"
	"log"
	"net/http"
	"time"
)

type callbackEvent struct {
	url     string
	payload StatusCallbackRequest
}

func (s *MockStore) start(fn func()) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	if s.closed {
		return
	}
	s.tasks.Add(1)
	go func() { defer s.tasks.Done(); fn() }()
}
func (s *MockStore) Close() {
	s.tasksMu.Lock()
	s.closed = true
	s.cancel()
	s.tasksMu.Unlock()
	s.tasks.Wait()
}
func waitFor(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
func (s *MockStore) enqueueLocked(base, restaurant, id, status string) {
	s.events = append(s.events, callbackEvent{url: fmt.Sprintf("%s/api/v1/integrations/restaurants/%s/orders/status", base, restaurant), payload: StatusCallbackRequest{ExternalOrderID: id, Status: status, EventID: uuid.NewString()}})
	if !s.dispatcherStarted {
		s.dispatcherStarted = true
		s.start(s.deliverCallbacks)
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *MockStore) QueueCallback(base, restaurant, id, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enqueueLocked(base, restaurant, id, status)
}

// Validate transition and append its event under one lock so concurrent manual
// requests cannot reorder the accepted events.
func (s *MockStore) NotifyStatus(base, restaurant, id, status string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok {
		return http.StatusNotFound
	}
	if !knownPartnerStatus(status) {
		return http.StatusBadRequest
	}
	if o.Status != status && !orders.CanTransition(orders.OrderStatus(o.Status), orders.OrderStatus(status)) {
		return http.StatusConflict
	}
	if o.Status != status {
		o.Status = status
		o.UpdatedAt = time.Now()
	}
	s.enqueueLocked(base, restaurant, id, status)
	return 0
}
func knownPartnerStatus(status string) bool {
	switch status {
	case "pending", "confirmed", "preparing", "ready", "delivering", "delivered", "cancelled":
		return true
	}
	return false
}
func (s *MockStore) deliverCallbacks() {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		s.mu.RLock()
		var event callbackEvent
		has := len(s.events) > 0
		if has {
			event = s.events[0]
		}
		s.mu.RUnlock()
		if !has {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
				continue
			}
		}
		body, _ := json.Marshal(event.payload)
		delay := 100 * time.Millisecond
		for {
			if s.ctx.Err() != nil {
				return
			}
			req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, event.url, bytes.NewReader(body))
			if err == nil && s.signCallback != nil {
				err = s.signCallback(req, body)
			}
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
				var resp *http.Response
				resp, err = client.Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						break
					}
					err = fmt.Errorf("HTTP %d", resp.StatusCode)
				}
			}
			log.Printf("Callback retry event=%s: %v", event.payload.EventID, err)
			if !waitFor(s.ctx, delay) {
				return
			}
			if delay < 5*time.Second {
				delay *= 2
				if delay > 5*time.Second {
					delay = 5 * time.Second
				}
			}
		}
		s.mu.Lock()
		s.events[0] = callbackEvent{}
		s.events = s.events[1:]
		s.mu.Unlock()
	}
}
