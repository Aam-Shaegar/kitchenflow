package core_http_request

import (
	"encoding/json"
	"fmt"
	"net/http"

	core_error "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

var requestValidator = validator.New()

// ParseUUID reads a path parameter as UUID. A malformed value is always
// the client's mistake, so we tag it as invalid input and every handler
// answers 400 instead of leaking a 500.
func ParseUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("parse uuid %q: %w", value, core_error.ErrInvalidArgument)
	}
	return id, nil
}

type validatable interface {
	Validate() error
}

func DecodeAndValidateRequest(r *http.Request, dest any) error {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode json: %w", core_error.ErrInvalidArgument)
	}

	v, ok := dest.(validatable)
	if ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("request validation: %w", core_error.ErrInvalidArgument)
		}
	} else {
		if err := requestValidator.Struct(dest); err != nil {
			return fmt.Errorf("request validation: %w", core_error.ErrInvalidArgument)
		}
	}

	return nil
}
