package core_http_response

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
