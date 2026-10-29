package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	core_error "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
)

func decodeJSON(r *http.Request, dest any) error {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode json: %w", core_error.ErrInvalidArgument)
	}
	return nil
}

func errInvalid() error {
	return core_error.ErrInvalidArgument
}
