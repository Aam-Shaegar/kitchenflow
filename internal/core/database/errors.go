package core_database

import (
	"errors"
	"fmt"
	domainerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
)

// WriteError translates expected constraint failures at the repository boundary.
// Keep the database cause available for diagnostics and errors.Is checks.
func WriteError(err error) error {
	if errors.Is(err, ErrUniqueViolation) {
		return fmt.Errorf("%w: %w", domainerrors.ErrConflict, err)
	}
	if errors.Is(err, ErrViolatesForeignKey) {
		return fmt.Errorf("%w: %w", domainerrors.ErrNotFound, err)
	}
	return err
}
func DeleteError(err error) error {
	if errors.Is(err, ErrViolatesForeignKey) {
		return fmt.Errorf("%w: %w", domainerrors.ErrConflict, err)
	}
	return WriteError(err)
}
