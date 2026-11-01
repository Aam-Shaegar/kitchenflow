package service

import (
	"context"
	"errors"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestRestaurantInputLimits(t *testing.T) {
	svc := NewRestaurantService(nil)
	for _, in := range []CreateRestaurantInput{
		{Name: " "}, {Name: "Restaurant", ExternalID: strings.Repeat("x", 129)},
		{Name: "Restaurant", Description: strings.Repeat("x", 2001)},
		{Name: "Restaurant", IntegrationBaseURL: strings.Repeat("x", 513)},
	} {
		if _, err := svc.Create(context.Background(), in); !errors.Is(err, coreerrors.ErrInvalidArgument) {
			t.Fatalf("error %v", err)
		}
	}

	id := uuid.New()
	svc = NewRestaurantService(routingRepository{restaurant: &domain.Restaurant{ID: id, Name: "Restaurant"}})
	for _, text := range []string{strings.Repeat("x", 513)} {
		if _, err := svc.Update(context.Background(), id, UpdateRestaurantInput{IntegrationBaseURL: &text}); !errors.Is(err, coreerrors.ErrInvalidArgument) {
			t.Fatalf("update limit: %v", err)
		}
	}

}
