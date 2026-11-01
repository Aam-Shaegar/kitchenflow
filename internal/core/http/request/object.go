package core_http_request

import (
	"bytes"
	"encoding/json"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"net/http"
)

// DecodeObject distinguishes absent fields from explicit null and rejects typos.
func DecodeObject(r *http.Request, dest any) error {
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
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
