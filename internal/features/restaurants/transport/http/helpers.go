package handler

import (
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	"net/http"
)

func decodeJSON(r *http.Request, dest any) error { return request.DecodeObject(r, dest) }
func errInvalid() error                          { return coreerrors.ErrInvalidArgument }
