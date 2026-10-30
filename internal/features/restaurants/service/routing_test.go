package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/repository"
	"github.com/google/uuid"
)

type routingRepository struct {
	repository.RestaurantRepository
	restaurant *domain.Restaurant
}

func (r routingRepository) GetByID(context.Context, uuid.UUID) (*domain.Restaurant, error) {
	return r.restaurant, nil
}

func TestOrderViewCarriesIntegrationURL(t *testing.T) {
	rest := &domain.Restaurant{ID: uuid.New(), IsOpen: true, IntegrationBaseURL: "https://restaurant.example"}
	svc := NewRestaurantService(routingRepository{restaurant: rest})
	info, err := svc.GetByIDForOrder(context.Background(), rest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.IntegrationBaseURL != rest.IntegrationBaseURL {
		t.Fatal("integration URL missing")
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "integration_base_url") {
		t.Fatalf("internal routing URL exposed in view: %s", raw)
	}
}
