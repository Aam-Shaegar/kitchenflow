package core_http_request

import (
	"bytes"
	"encoding/json"
	"errors"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"io"
	"net/http"
)

const MaxBodyBytes int64 = 1024 * 1024

func DecodeObject(r *http.Request, dest any) error { return DecodeJSON(r.Body, dest, MaxBodyBytes) }

// DecodeJSON accepts exactly one object, with no null fields or unknown keys.
func DecodeJSON(reader io.Reader, dest any, limit int64) error {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) || int64(len(raw)) > limit {
		return coreerrors.ErrRequestTooLarge
	}
	if err != nil {
		return coreerrors.ErrInvalidArgument
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return coreerrors.ErrInvalidArgument
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return coreerrors.ErrInvalidArgument
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return coreerrors.ErrInvalidArgument
	}
	return nil
}
func JSONErrorStatus(err error) int {
	if errors.Is(err, coreerrors.ErrRequestTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
