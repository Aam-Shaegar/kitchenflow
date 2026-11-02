package callbackauth

import (
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSignedCallbacksAndRotation(t *testing.T) {
	restaurant := uuid.New()
	secret := []byte(strings.Repeat("secret-key", 4))
	entries := []key{{ID: "old", Restaurant: restaurant, Secret: base64.StdEncoding.EncodeToString(secret), Expires: time.Now().Add(time.Hour)}, {ID: "new", Restaurant: restaurant, Secret: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("new-secret", 4))), Expires: time.Now().Add(time.Hour)}}
	file := filepath.Join(t.TempDir(), "keys.json")
	raw, _ := json.Marshal(entries)
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	keys, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/integrations/restaurants/" + restaurant.String() + "/orders/status"
	body := []byte(`{"external_order_id":"order","status":"confirmed","event_id":"stable"}`)
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{name: "valid", want: 204}, {name: "repeat", want: 204},
		{name: "unsigned", mutate: func(r *http.Request) { r.Header.Del("X-Callback-Signature") }, want: 401},
		{name: "body tampering", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{"status":"cancelled"}`)) }, want: 401},
		{name: "path tampering", mutate: func(r *http.Request) { r.URL.Path = strings.Replace(path, restaurant.String(), uuid.NewString(), 1) }, want: 401},
		{name: "wrong restaurant even when signed", mutate: func(r *http.Request) {
			r.URL.Path = strings.Replace(path, restaurant.String(), uuid.NewString(), 1)
			sign, _ := keys.Signer("old", restaurant)
			_ = sign(r, body)
		}, want: 401},
		{name: "unknown key", mutate: func(r *http.Request) { r.Header.Set("X-Callback-Key-ID", "unknown") }, want: 401},
		{name: "stale", mutate: func(r *http.Request) {
			stamp := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
			r.Header.Set("X-Callback-Timestamp", stamp)
			r.Header.Set("X-Callback-Signature", signature(secret, stamp, "POST", path, body))
		}, want: 401},
		{name: "duplicate header", mutate: func(r *http.Request) { r.Header.Add("X-Callback-Key-ID", "old") }, want: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
			sign, _ := keys.Signer("old", restaurant)
			if err := sign(req, body); err != nil {
				t.Fatal(err)
			}
			if tc.mutate != nil {
				tc.mutate(req)
			}
			mux := http.NewServeMux()
			mux.Handle("POST /api/v1/integrations/restaurants/{restaurant_id}/orders/status", keys.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, _ := io.ReadAll(r.Body)
				if string(got) != string(body) {
					t.Fatal("body not restored")
				}
				w.WriteHeader(204)
			})))
			rw := httptest.NewRecorder()
			mux.ServeHTTP(rw, req)
			if rw.Code != tc.want {
				t.Fatalf("HTTP %d want %d", rw.Code, tc.want)
			}
		})
	}
	// Overlap: both keys work; removing old key revokes it immediately in loaded config.
	for _, id := range []string{"old", "new"} {
		sign, err := keys.Signer(id, restaurant)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
		_ = sign(req, body)
		req.SetPathValue("restaurant_id", restaurant.String())
		rw := httptest.NewRecorder()
		keys.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(rw, req)
		if rw.Code != 204 {
			t.Fatal("rotation overlap failed")
		}
	}
	expiredRequest := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
	expiredRequest.SetPathValue("restaurant_id", restaurant.String())
	oldSigner, _ := keys.Signer("old", restaurant)
	_ = oldSigner(expiredRequest, body)
	keys.keys["old"] = key{Restaurant: restaurant, Expires: time.Now().Add(-time.Hour), secret: secret}
	rw := httptest.NewRecorder()
	keys.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("expired key reached handler") })).ServeHTTP(rw, expiredRequest)
	if rw.Code != 401 {
		t.Fatal("expired receiver key accepted")
	}

	if _, err := keys.Signer("old", restaurant); err == nil {
		t.Fatal("expired signer accepted")
	}
	if _, err := keys.Signer("new", uuid.New()); err == nil {
		t.Fatal("signer bound to wrong restaurant")
	}
}

func TestSignatureSurvivesAPIPrefixStripping(t *testing.T) {
	rid := uuid.New()
	secret := []byte(strings.Repeat("k", 32))
	keys := &Keys{keys: map[string]key{"test": {Restaurant: rid, Expires: time.Now().Add(time.Hour), secret: secret}}}
	body := []byte(`{"event_id":"same"}`)
	path := "/api/v1/integrations/restaurants/" + rid.String() + "/orders/status"
	req := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
	sign, _ := keys.Signer("test", rid)
	_ = sign(req, body)
	inner := http.NewServeMux()
	inner.Handle("POST /integrations/restaurants/{restaurant_id}/orders/status", keys.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	outer := http.NewServeMux()
	outer.Handle("/api/v1/", http.StripPrefix("/api/v1", inner))
	rw := httptest.NewRecorder()
	outer.ServeHTTP(rw, req)
	if rw.Code != 204 {
		t.Fatalf("signed API callback HTTP %d", rw.Code)
	}
}
