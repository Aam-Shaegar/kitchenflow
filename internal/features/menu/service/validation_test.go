package service

import (
	"context"
	"errors"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestCreateValidationBeforeDatabase(t *testing.T) {
	svc := NewMenuService(nil)
	if _, err := svc.CreateCategory(context.Background(), uuid.New(), "Category", -1); !errors.Is(err, coreerrors.ErrInvalidArgument) {
		t.Fatalf("position: %v", err)
	}
	for _, in := range []CreateItemInput{{Name: "Item", Description: strings.Repeat("я", 2001)}, {Name: "Item", ExternalID: strings.Repeat("я", 129)}} {
		if _, err := svc.CreateItem(context.Background(), in); !errors.Is(err, coreerrors.ErrInvalidArgument) {
			t.Fatalf("create: %v", err)
		}
	}
}
