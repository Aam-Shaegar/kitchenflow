package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/repository"
)

type RestaurantService struct {
	repo repository.RestaurantRepository
}

func NewRestaurantService(repo repository.RestaurantRepository) *RestaurantService {
	return &RestaurantService{repo: repo}
}

func (s *RestaurantService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Restaurant, error) {
	rest, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rest == nil {
		return nil, errors.ErrNotFound
	}
	return rest, nil
}

func (s *RestaurantService) GetByIDForOrder(ctx context.Context, id uuid.UUID) (*domain.RestaurantInfo, error) {
	return s.GetInfoByID(ctx, id)
}

func (s *RestaurantService) GetInfoByID(ctx context.Context, id uuid.UUID) (*domain.RestaurantInfo, error) {
	rest, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rest == nil {
		return nil, errors.ErrNotFound
	}
	return &domain.RestaurantInfo{
		ID:          rest.ID,
		ExternalID:  rest.ExternalID,
		Name:        rest.Name,
		Description: rest.Description,
		IsOpen:      rest.IsOpen,
	}, nil
}

func (s *RestaurantService) List(ctx context.Context) ([]domain.RestaurantListItem, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []domain.RestaurantListItem{}, nil
	}
	return items, nil
}

func (s *RestaurantService) ListFiltered(ctx context.Context, onlyOpen bool) ([]domain.RestaurantListItem, error) {
	return s.ListFilteredPaginated(ctx, onlyOpen, 100, 0)
}

func (s *RestaurantService) ListFilteredPaginated(ctx context.Context, onlyOpen bool, limit, offset int) ([]domain.RestaurantListItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	items, err := s.repo.ListPaginated(ctx, onlyOpen, limit, offset)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []domain.RestaurantListItem{}, nil
	}
	return items, nil
}

type CreateRestaurantInput struct {
	Name               string
	Description        string
	ExternalID         string
	IntegrationBaseURL string
	IsOpen             bool
}

func (s *RestaurantService) Create(ctx context.Context, in CreateRestaurantInput) (*domain.Restaurant, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
		return nil, errors.ErrInvalidArgument
	}
	if len(in.Description) > 2000 {
		return nil, errors.ErrInvalidArgument
	}
	externalID := strings.TrimSpace(in.ExternalID)
	if externalID == "" {
		externalID = "rest-" + uuid.NewString()[:8]
	}
	now := time.Now()
	rest := &domain.Restaurant{
		ID:                 uuid.New(),
		ExternalID:         externalID,
		Name:               strings.TrimSpace(in.Name),
		Description:        in.Description,
		IsOpen:             true,
		IntegrationBaseURL: strings.TrimSpace(in.IntegrationBaseURL),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	// Fresh restaurants always open their doors; closing them is an explicit Update call.
	if err := s.repo.Create(ctx, rest); err != nil {
		return nil, err
	}
	return rest, nil
}

type UpdateRestaurantInput struct {
	Name               *string
	Description        *string
	IsOpen             *bool
	IntegrationBaseURL *string
}

func (s *RestaurantService) Update(ctx context.Context, id uuid.UUID, in UpdateRestaurantInput) (*domain.Restaurant, error) {
	rest, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" || len(*in.Name) > 200 {
			return nil, errors.ErrInvalidArgument
		}
		rest.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		if len(*in.Description) > 2000 {
			return nil, errors.ErrInvalidArgument
		}
		rest.Description = *in.Description
	}
	if in.IsOpen != nil {
		rest.IsOpen = *in.IsOpen
	}
	if in.IntegrationBaseURL != nil {
		rest.IntegrationBaseURL = strings.TrimSpace(*in.IntegrationBaseURL)
	}
	rest.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, rest); err != nil {
		return nil, err
	}
	return rest, nil
}
