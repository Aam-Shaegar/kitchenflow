package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
)

type HTTPClient struct {
	allowedOrigins map[string]bool
	privateCIDRs   []netip.Prefix
	httpClient     *http.Client
}

// Allowed origins and private networks are operator-owned configuration, never
// supplied through restaurant APIs. An empty allowlist disables dispatch.
func NewHTTPClient(timeout time.Duration, origins, privateCIDRs []string) (*HTTPClient, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("restaurant timeout must be positive")
	}
	c := &HTTPClient{allowedOrigins: make(map[string]bool)}
	for _, raw := range origins {
		u, err := parseEndpoint(strings.TrimSpace(raw))
		if err != nil || u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("invalid restaurant allowed origin %q", raw)
		}
		c.allowedOrigins[origin(u)] = true
	}
	for _, raw := range privateCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid restaurant private CIDR %q: %w", raw, err)
		}
		c.privateCIDRs = append(c.privateCIDRs, prefix)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Environment proxies must not bypass destination checks.
	transport.DialContext = c.dialContext
	c.httpClient = &http.Client{
		Timeout: timeout, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if len(raw) > 512 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" {
		return nil, fmt.Errorf("invalid restaurant endpoint")
	}
	// Validate port even before a network request is attempted.
	port, err := strconv.Atoi(effectivePort(u))
	if err != nil || port < 1 || port > 65535 || strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid restaurant port")
	}
	return u, nil
}

func effectivePort(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func origin(u *url.URL) string {
	return u.Scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), effectivePort(u))
}

func (c *HTTPClient) ValidateEndpoint(baseURL string) error {
	u, err := parseEndpoint(baseURL)
	if err != nil || !c.allowedOrigins[origin(u)] {
		return coreerrors.ErrIntegrationUnavailable
	}
	return nil
}

// Special-use networks must never be reachable, even through a broad private CIDR.
var blockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func (c *HTTPClient) allowedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, prefix := range blockedNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	if !ip.IsGlobalUnicast() && !ip.IsLoopback() {
		return false
	}
	if !ip.IsPrivate() && !ip.IsLoopback() {
		return true
	}
	for _, prefix := range c.privateCIDRs {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (c *HTTPClient) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("restaurant hostname resolved to no addresses")
	}
	// Resolve once and dial the checked IP directly, preventing DNS rebinding
	// between validation and connection. Mixed public/private answers fail closed.
	for _, ip := range ips {
		if !c.allowedIP(ip) {
			return nil, fmt.Errorf("restaurant destination blocked")
		}
	}
	var dialer net.Dialer
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	return nil, err
}

func (c *HTTPClient) CreateOrder(ctx context.Context, baseURL string, req domain.CreateOrderRequest) (*domain.CreateOrderResponse, error) {
	return c.post(ctx, baseURL, "api/v1/restaurant/orders", req)
}

func (c *HTTPClient) CancelOrder(ctx context.Context, baseURL, externalOrderID string) (*domain.CreateOrderResponse, error) {
	return c.post(ctx, baseURL, "api/v1/restaurant/orders/cancel", struct {
		ExternalOrderID string `json:"external_order_id"`
	}{externalOrderID})
}

func (c *HTTPClient) post(ctx context.Context, baseURL, path string, req any) (*domain.CreateOrderResponse, error) {
	if err := c.ValidateEndpoint(baseURL); err != nil {
		return nil, err
	}
	endpoint, _ := url.Parse(baseURL)
	endpoint = endpoint.JoinPath(path)
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil, coreerrors.ErrConflict
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	var result domain.CreateOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
