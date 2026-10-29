package core_http_response

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	core_error "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	core_logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"

	"go.uber.org/zap"
)

type HTTPResponseHandler struct {
	log *core_logger.Logger
	rw  http.ResponseWriter
}

func NewHTTPResponseHandler(log *core_logger.Logger, rw http.ResponseWriter) *HTTPResponseHandler {
	return &HTTPResponseHandler{
		log: log,
		rw:  rw,
	}
}

func (h *HTTPResponseHandler) NoContentResponse() {
	h.rw.WriteHeader(http.StatusNoContent)
}

func (h *HTTPResponseHandler) ErrorResponse(err error, msg string) {
	var (
		statusCode int
		logFunc    func(string, ...zap.Field)
	)

	switch {
	case errors.Is(err, core_error.ErrInvalidArgument):
		statusCode = http.StatusBadRequest
		logFunc = h.log.Warn
	case errors.Is(err, core_error.ErrNotFound):
		statusCode = http.StatusNotFound
		logFunc = h.log.Debug
	case errors.Is(err, core_error.ErrConflict):
		statusCode = http.StatusConflict
		logFunc = h.log.Warn
	case errors.Is(err, core_error.ErrUnauthorized):
		statusCode = http.StatusUnauthorized
		logFunc = h.log.Warn
	case isTransitionConflict(err):
		statusCode = http.StatusConflict
		logFunc = h.log.Warn
	default:
		statusCode = http.StatusInternalServerError
		logFunc = h.log.Error
	}
	logFunc(msg, zap.Error(err))
	h.errorResponse(statusCode, err, msg)
}

// isTransitionConflict tells a state-machine refusal apart from a real bug.
// Matching by message is deliberate: core cannot import the orders domain
// without creating an import cycle, so the domain error carries a stable text.
func isTransitionConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "invalid order status transition") ||
		strings.Contains(msg, "invalid state transition")
}

func (h *HTTPResponseHandler) PanicResponse(p any, msg string) {
	statusCode := http.StatusInternalServerError
	err := fmt.Errorf("unexpected panic: %v", p)

	h.log.Error(msg, zap.Error(err))
	h.errorResponse(statusCode, err, msg)
}

func (h *HTTPResponseHandler) errorResponse(statusCode int, _ error, msg string) {
	response := map[string]string{
		"message": msg,
		"error":   http.StatusText(statusCode),
	}
	h.JSONResponse(response, statusCode)
}

func (h *HTTPResponseHandler) JSONResponse(responseBody any, statusCode int) {
	h.rw.Header().Set("Content-Type", "application/json")
	h.rw.WriteHeader(statusCode)
	if err := json.NewEncoder(h.rw).Encode(responseBody); err != nil {
		h.log.Error("write HTTP response", zap.Error(err))
	}
}
