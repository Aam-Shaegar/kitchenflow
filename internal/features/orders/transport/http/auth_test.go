package handler

import (
	"context"
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

	auth "github.com/Aam-Shaegar/kitchenflow/internal/core/http/middleware"
	logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/service"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type identityRepo struct {
	repository.OrderRepository
	own uuid.UUID
	t   *testing.T
}

func (r identityRepo) GetByIDAndUserID(_ context.Context, id uuid.UUID, user string) (*domain.Order, error) {
	if user != "authenticated-user" {
		r.t.Fatalf("query identity %q", user)
	}
	if id != r.own {
		return nil, nil
	}
	return &domain.Order{ID: id, UserID: user}, nil
}
func TestOrderReadIgnoresForgedIdentity(t *testing.T) {
	token := strings.Repeat("test-token", 5)
	hash := sha256.Sum256([]byte(token))
	raw, err := json.Marshal([]map[string]any{{"subject": "authenticated-user", "role": "user", "token_sha256": hex.EncodeToString(hash[:]), "expires_at": time.Now().Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := auth.LoadAuth(file)
	if err != nil {
		t.Fatal(err)
	}
	own, other := uuid.New(), uuid.New()
	h := NewOrderHandler(service.NewOrderService(identityRepo{own: own, t: t}, nil, nil, nil, nil))
	for _, id := range []uuid.UUID{own, other} {
		req := httptest.NewRequest("GET", "/", nil)
		req.SetPathValue("order_id", id.String())
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-User-ID", "victim")
		req = req.WithContext(logger.ToContext(req.Context(), &logger.Logger{Logger: zap.NewNop()}))
		rw := httptest.NewRecorder()
		a.User()(http.HandlerFunc(h.GetOrder)).ServeHTTP(rw, req)
		want := 200
		if id == other {
			want = 404
		}
		if rw.Code != want {
			t.Fatalf("HTTP %d want %d", rw.Code, want)
		}
	}
}
