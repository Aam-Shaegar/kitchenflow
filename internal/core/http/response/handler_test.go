package core_http_response

import (
	"encoding/json"
	"fmt"
	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	orders "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	corelogger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"go.uber.org/zap"
)

func TestIntegrationUnavailableResponse(t *testing.T) {
	rw := httptest.NewRecorder()
	h := NewHTTPResponseHandler(&corelogger.Logger{Logger: zap.NewNop()}, rw)
	h.ErrorResponse(fmt.Errorf("endpoint policy: %w", coreerrors.ErrIntegrationUnavailable), "failed to create order")
	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP status: %d", rw.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != "restaurant integration unavailable" {
		t.Fatalf("response: %v", body)
	}
}

func TestConstraintResponses(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{
		{db.WriteError(fmt.Errorf("insert: %w", db.ErrUniqueViolation)), 409},
		{&orders.OrderStatusTransitionError{From: orders.OrderStatusPreparing, To: orders.OrderStatusCancelled}, 409},
		{db.WriteError(fmt.Errorf("insert: %w", db.ErrViolatesForeignKey)), 404},
		{db.DeleteError(fmt.Errorf("delete: %w", db.ErrViolatesForeignKey)), 409},
		{db.WriteError(fmt.Errorf("connection lost: %w", db.ErrUnknown)), 500},
	} {
		rw := httptest.NewRecorder()
		NewHTTPResponseHandler(&corelogger.Logger{Logger: zap.NewNop()}, rw).ErrorResponse(tc.err, "operation failed")
		if rw.Code != tc.code {
			t.Fatalf("HTTP %d want %d", rw.Code, tc.code)
		}
		if strings.Contains(rw.Body.String(), "violation") {
			t.Fatal("database detail exposed")
		}
	}
}
