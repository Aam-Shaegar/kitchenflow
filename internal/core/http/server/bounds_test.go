package core_http_server

import (
	request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerBodyAndHeaderLimits(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value struct {
			Name string `json:"name"`
		}
		if err := request.DecodeObject(r, &value); err != nil {
			w.WriteHeader(request.JSONErrorStatus(err))
			return
		}
		w.WriteHeader(204)
	})
	server := httptest.NewUnstartedServer(handler)
	ApplyBounds(server.Config)
	server.Start()
	defer server.Close()
	for _, unknown := range []bool{false, true} {
		body := `{"name":"` + strings.Repeat("x", int(request.MaxBodyBytes)) + `"}`
		var reader io.Reader = strings.NewReader(body)
		if unknown {
			reader = io.NopCloser(strings.NewReader(body))
		}
		req, _ := http.NewRequest("POST", server.URL, reader)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 413 {
			t.Fatalf("chunked=%t status=%d", unknown, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("X-Large", strings.Repeat("x", 70*1024))
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 431 {
		t.Fatalf("large header=%d", resp.StatusCode)
	}
	if server.Config.ReadTimeout <= 0 || server.Config.WriteTimeout <= 0 || server.Config.IdleTimeout <= 0 {
		t.Fatal("missing server timeouts")
	}
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(7 * time.Second))
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: "))
	start := time.Now()
	var b [1]byte
	_, err = conn.Read(b[:])
	if err == nil {
		t.Fatal("partial header was accepted")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("header timeout not enforced")
	}
}
