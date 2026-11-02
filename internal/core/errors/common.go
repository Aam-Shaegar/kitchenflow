package errors

import "errors"

var (
	ErrRequestTooLarge        = errors.New("request body too large")
	ErrIntegrationUnavailable = errors.New("restaurant integration unavailable")
	ErrNotFound               = errors.New("not found")
	ErrInvalidArgument        = errors.New("invalid argument")
	ErrConflict               = errors.New("conflict")
	ErrUnauthorized           = errors.New("unauthorized")
	ErrInternal               = errors.New("internal error")
)
