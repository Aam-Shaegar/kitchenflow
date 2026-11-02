package core_http_middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/google/uuid"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Principal struct {
	Subject     string      `json:"subject"`
	Role        string      `json:"role"`
	Restaurants []uuid.UUID `json:"restaurant_ids"`
	ExpiresAt   time.Time   `json:"expires_at"`
}
type credential struct {
	Principal
	Hash string `json:"token_sha256"`
}
type Auth struct{ credentials map[[32]byte]Principal }
type principalKey struct{}

func Subject(ctx context.Context) string {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p.Subject
}
func LoadAuth(path string) (*Auth, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open auth credentials: %w", err)
	}
	defer file.Close()
	var entries []credential
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode auth credentials: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("trailing auth credentials data")
	}
	a := &Auth{credentials: make(map[[32]byte]Principal)}
	if len(entries) == 0 {
		return nil, fmt.Errorf("auth credentials are empty")
	}
	for _, entry := range entries {
		raw, err := hex.DecodeString(entry.Hash)
		if err != nil || len(raw) != 32 || strings.TrimSpace(entry.Subject) == "" || len(entry.Subject) > 128 || entry.ExpiresAt.IsZero() {
			return nil, fmt.Errorf("invalid credential")
		}
		if entry.Role != "user" && entry.Role != "partner" && entry.Role != "admin" {
			return nil, fmt.Errorf("invalid credential role")
		}
		if entry.Role == "partner" && len(entry.Restaurants) == 0 {
			return nil, fmt.Errorf("partner has no restaurants")
		}
		for _, id := range entry.Restaurants {
			if id == uuid.Nil {
				return nil, fmt.Errorf("invalid restaurant ID")
			}
		}
		var hash [32]byte
		copy(hash[:], raw)
		if _, ok := a.credentials[hash]; ok {
			return nil, fmt.Errorf("duplicate credential")
		}
		a.credentials[hash] = entry.Principal
	}
	return a, nil
}
func (a *Auth) protect(role string, restaurant func(*http.Request) (uuid.UUID, error)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) < 32 || len(r.Header.Values("Authorization")) != 1 {
				authError(w, 401)
				return
			}
			hash := sha256.Sum256([]byte(parts[1]))
			p, ok := a.credentials[hash]
			if !ok || !time.Now().Before(p.ExpiresAt) {
				authError(w, 401)
				return
			}
			if p.Role != role && p.Role != "admin" {
				authError(w, 403)
				return
			}
			if restaurant != nil && p.Role != "admin" {
				id, err := restaurant(r)
				if err != nil {
					if errors.Is(err, coreerrors.ErrNotFound) || errors.Is(err, coreerrors.ErrInvalidArgument) {
						authError(w, 404)
					} else {
						authError(w, 500)
					}
					return
				}
				allowed := false
				for _, owned := range p.Restaurants {
					if id == owned {
						allowed = true
					}
				}
				if !allowed {
					authError(w, 403)
					return
				}
			}
			// Administrators manage partner resources, but do not impersonate customers.
			if role == "user" && p.Role != "user" {
				authError(w, 403)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		})
	}
}
func (a *Auth) User() Middleware  { return a.protect("user", nil) }
func (a *Auth) Admin() Middleware { return a.protect("admin", nil) }
func (a *Auth) Restaurant(resolve func(*http.Request) (uuid.UUID, error)) Middleware {
	return a.protect("partner", resolve)
}
func RestaurantPath(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("restaurant_id"))
	if err != nil {
		return uuid.Nil, coreerrors.ErrInvalidArgument
	}
	return id, nil
}
func authError(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	if code == 401 {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": http.StatusText(code), "message": http.StatusText(code)})
}
