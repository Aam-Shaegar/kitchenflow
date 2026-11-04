package core_http_middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthIdentityAndRestaurantIsolation(t *testing.T) {
	own, other := uuid.New(), uuid.New()
	tokens := map[string]string{"user": strings.Repeat("u", 43), "partner": strings.Repeat("p", 43), "admin": strings.Repeat("a", 43), "expired": strings.Repeat("e", 43)}
	entries := make([]credential, 0, len(tokens))
	for role, token := range tokens {
		hash := sha256.Sum256([]byte(token))
		expires := time.Now().Add(time.Hour)
		actualRole := role
		if role == "expired" {
			actualRole = "user"
			expires = time.Now().Add(-time.Hour)
		}
		entries = append(entries, credential{Hash: hex.EncodeToString(hash[:]), Principal: Principal{Subject: role + "-identity", Role: actualRole, Restaurants: []uuid.UUID{own}, ExpiresAt: expires}})
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	auth, err := LoadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token, role string
		restaurant        uuid.UUID
		want              int
	}{
		{name: "header alone", role: "user", want: 401},
		{name: "unknown token", token: strings.Repeat("x", 43), role: "user", want: 401},
		{name: "expired", token: tokens["expired"], role: "user", want: 401},
		{name: "user cannot impersonate", token: tokens["user"], role: "user", want: 200},
		{name: "user cannot create restaurants", token: tokens["user"], role: "admin", want: 403},
		{name: "partner cannot create restaurants", token: tokens["partner"], role: "admin", want: 403},
		{name: "admin creates restaurants", token: tokens["admin"], role: "admin", want: 200},
		{name: "partner own restaurant", token: tokens["partner"], role: "partner", restaurant: own, want: 200},
		{name: "partner other restaurant", token: tokens["partner"], role: "partner", restaurant: other, want: 403},
		{name: "user cannot read partner orders", token: tokens["user"], role: "partner", restaurant: own, want: 403},
		{name: "partner cannot impersonate customer", token: tokens["partner"], role: "user", want: 403},
		{name: "admin cannot impersonate customer", token: tokens["admin"], role: "user", want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var guard Middleware
			switch tc.role {
			case "user":
				guard = auth.User()
			case "admin":
				guard = auth.Admin()
			default:
				guard = auth.Restaurant(RestaurantPath)
			}
			called := false
			h := guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if Subject(r.Context()) == "forged" || Subject(r.Context()) == "" {
					t.Fatal("untrusted identity")
				}
				w.WriteHeader(200)
			}))
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-User-ID", "forged")
			req.SetPathValue("restaurant_id", tc.restaurant.String())
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, req)
			if rw.Code != tc.want || called != (tc.want == 200) {
				t.Fatalf("HTTP %d called=%t", rw.Code, called)
			}
		})
	}
}
func TestAuthConfigurationFailsClosed(t *testing.T) {
	if _, err := LoadAuth(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing config accepted")
	}
	for _, raw := range []string{`[]`, `{}`, `null`, `[{"token_sha256":"bad","subject":"u","role":"user"}]`} {
		path := filepath.Join(t.TempDir(), "auth.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadAuth(path); err == nil {
			t.Fatalf("bad config accepted %s", raw)
		}
	}
}
