package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
)

func TestRoutesOnlyToSelectedRestaurant(t *testing.T) {
	servers := make([]*httptest.Server, 2)
	for i := range servers {
		want := []string{"order-a", "order-b"}[i]
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req domain.CreateOrderRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if r.Method != "POST" || r.URL.Path != "/partner/api/v1/restaurant/orders" || req.ExternalOrderID != want {
				t.Errorf("wrong restaurant request: %s %s %+v", r.Method, r.URL.Path, req)
			}
			_ = json.NewEncoder(w).Encode(domain.CreateOrderResponse{ExternalOrderID: req.ExternalOrderID, Status: "confirmed"})
		}))
		defer servers[i].Close()
	}
	c, err := NewHTTPClient(time.Second, []string{servers[0].URL, servers[1].URL}, []string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range servers {
		id := []string{"order-a", "order-b"}[i]
		resp, err := c.CreateOrder(context.Background(), s.URL+"/partner/", domain.CreateOrderRequest{ExternalOrderID: id})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ExternalOrderID != id {
			t.Fatalf("wrong response: %+v", resp)
		}
	}
}

func TestEndpointPolicy(t *testing.T) {
	c, err := NewHTTPClient(time.Second, []string{"https://partner.example:443"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "partner.example", "file:///etc/passwd", "ftp://partner.example", "https://user:password@partner.example", "https://partner.example?secret=1", "https://partner.example?", "https://partner.example#", "https://partner.example#fragment", "http://partner.example", "https://partner.example:444", "https://partner.example.evil.test", "https://169.254.169.254", "https://partner.example:99999", "https://partner.example:0", "https://partner.example:"} {
		t.Run(raw, func(t *testing.T) {
			if c.ValidateEndpoint(raw) == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
	if err := c.ValidateEndpoint("https://partner.example/prefix/"); err != nil {
		t.Fatal(err)
	}
	disabled, err := NewHTTPClient(time.Second, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.ValidateEndpoint("https://partner.example") == nil {
		t.Fatal("empty allowlist allowed traffic")
	}
	for _, raw := range []string{"http://partner.example/path", "http://user@partner.example", "http://partner.example?", "bad-url"} {
		if _, err := NewHTTPClient(time.Second, []string{raw}, nil); err == nil {
			t.Fatalf("bad configuration accepted: %s", raw)
		}
	}
	if _, err := NewHTTPClient(time.Second, nil, []string{"invalid"}); err == nil {
		t.Fatal("bad CIDR accepted")
	}
}

func TestBlockedDestinationsAndRedirects(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(201) }))
	defer target.Close()
	c, err := NewHTTPClient(time.Second, []string{target.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateOrder(context.Background(), target.URL, domain.CreateOrderRequest{}); err == nil {
		t.Fatal("private IP allowed without CIDR")
	}
	// Unapproved origin remains blocked even when its private network is allowed.
	c, err = NewHTTPClient(time.Second, nil, []string{"127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateOrder(context.Background(), target.URL, domain.CreateOrderRequest{}); err == nil {
		t.Fatal("unapproved origin allowed")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	// Even a redirect to another approved endpoint must not be followed.
	c, err = NewHTTPClient(time.Second, []string{redirect.URL, target.URL}, []string{"127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateOrder(context.Background(), redirect.URL, domain.CreateOrderRequest{}); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls.Load() != 0 {
		t.Fatalf("blocked destination received %d calls", calls.Load())
	}
}

func TestIPPolicy(t *testing.T) {
	c, err := NewHTTPClient(time.Second, nil, []string{"127.0.0.0/8", "10.0.0.0/24", "::1/128", "0.0.0.0/0", "::/0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"0.0.0.0", "0.0.0.1", "169.254.169.254", "224.0.0.1", "::", "fe80::1", "ff02::1", "100.100.100.200", "198.18.0.1", "240.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::"} {
		if c.allowedIP(netip.MustParseAddr(raw)) {
			t.Errorf("unsafe IP allowed: %s", raw)
		}
	}
	strict, err := NewHTTPClient(time.Second, nil, []string{"10.0.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ip   string
		want bool
	}{{"10.0.0.1", true}, {"10.0.1.1", false}, {"127.0.0.1", false}, {"::ffff:127.0.0.1", false}, {"::1", false}, {"192.168.0.1", false}, {"8.8.8.8", true}} {
		if got := strict.allowedIP(netip.MustParseAddr(tc.ip)); got != tc.want {
			t.Errorf("IP %s: %t", tc.ip, got)
		}
	}
}

func TestCancelOrderHTTPContract(t *testing.T) {
	for _, code := range []int{200, 409, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/partner/api/v1/restaurant/orders/cancel" || r.Method != "POST" {
				t.Errorf("wrong cancellation route %s %s", r.Method, r.URL.Path)
			}
			var req struct {
				ExternalOrderID string `json:"external_order_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.ExternalOrderID != "order" {
				t.Error("wrong order ID")
			}
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(domain.CreateOrderResponse{ExternalOrderID: "order", Status: "cancelled"})
		}))
		c, err := NewHTTPClient(time.Second, []string{server.URL}, []string{"127.0.0.0/8"})
		if err != nil {
			t.Fatal(err)
		}
		result, err := c.CancelOrder(context.Background(), server.URL+"/partner", "order")
		if code == 200 {
			if err != nil || result.Status != "cancelled" {
				t.Fatalf("result %+v error %v", result, err)
			}
		} else if err == nil {
			t.Fatal("accepted failure response")
		}
		server.Close()
	}
}
