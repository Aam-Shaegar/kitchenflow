package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/repository"
)

type MenuService struct {
	repo repository.MenuRepository
}

func NewMenuService(repo repository.MenuRepository) *MenuService {
	return &MenuService{repo: repo}
}

func (s *MenuService) GetMenuByRestaurantID(ctx context.Context, restaurantID uuid.UUID) (*domain.Menu, error) {
	categories, err := s.repo.GetCategoriesByRestaurantID(ctx, restaurantID)
	if err != nil {
		return nil, err
	}
	if len(categories) == 0 {
		return nil, errors.ErrNotFound
	}
	return &domain.Menu{
		RestaurantID: restaurantID,
		Categories:   categories,
	}, nil
}

func (s *MenuService) GetItemByID(ctx context.Context, id uuid.UUID) (*domain.MenuItem, error) {
	item, err := s.repo.GetItemByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errors.ErrNotFound
	}
	return item, nil
}

func (s *MenuService) GetItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.MenuItem, error) {
	return s.repo.GetItemsByIDs(ctx, ids)
}

func (s *MenuService) CreateCategory(ctx context.Context, restaurantID uuid.UUID, name string, position int) (*domain.MenuCategory, error) {
	if strings.TrimSpace(name) == "" || len(name) > 120 {
		return nil, errors.ErrInvalidArgument
	}
	now := time.Now()
	cat := &domain.MenuCategory{
		ID:           uuid.New(),
		RestaurantID: restaurantID,
		Name:         strings.TrimSpace(name),
		Position:     position,
		CreatedAt:    now,
		UpdatedAt:    now,
		Items:        []domain.MenuItem{},
	}
	if err := s.repo.CreateCategory(ctx, cat); err != nil {
		return nil, err
	}
	return cat, nil
}

type CreateItemInput struct {
	RestaurantID uuid.UUID
	CategoryID   uuid.UUID
	ExternalID   string
	Name         string
	Description  string
	Price        int64
	IsAvailable  bool
}

func (s *MenuService) CreateItem(ctx context.Context, in CreateItemInput) (*domain.MenuItem, error) {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
		return nil, errors.ErrInvalidArgument
	}
	if in.Price < 0 || in.Price > 10_000_000_00 {
		return nil, errors.ErrInvalidArgument
	}
	cat, err := s.repo.GetCategoryByID(ctx, in.CategoryID)
	if err != nil {
		return nil, err
	}
	if cat == nil || cat.RestaurantID != in.RestaurantID {
		return nil, errors.ErrInvalidArgument
	}
	now := time.Now()
	externalID := strings.TrimSpace(in.ExternalID)
	if externalID == "" {
		externalID = "mi-" + uuid.NewString()[:8]
	}
	item := &domain.MenuItem{
		ID:           uuid.New(),
		RestaurantID: in.RestaurantID,
		CategoryID:   in.CategoryID,
		ExternalID:   externalID,
		Name:         strings.TrimSpace(in.Name),
		Description:  in.Description,
		Price:        in.Price,
		IsAvailable:  true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateItem(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

type UpdateItemInput struct {
	Name        *string
	Description *string
	Price       *int64
	IsAvailable *bool
	CategoryID  *uuid.UUID
}

func (s *MenuService) UpdateItem(ctx context.Context, id uuid.UUID, in UpdateItemInput) (*domain.MenuItem, error) {
	item, err := s.repo.GetItemByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errors.ErrNotFound
	}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" || len(*in.Name) > 200 {
			return nil, errors.ErrInvalidArgument
		}
		item.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		if len(*in.Description) > 2000 {
			return nil, errors.ErrInvalidArgument
		}
		item.Description = *in.Description
	}
	if in.Price != nil {
		if *in.Price < 0 || *in.Price > 10_000_000_00 {
			return nil, errors.ErrInvalidArgument
		}
		item.Price = *in.Price
	}
	if in.IsAvailable != nil {
		item.IsAvailable = *in.IsAvailable
	}
	if in.CategoryID != nil {
		cat, err := s.repo.GetCategoryByID(ctx, *in.CategoryID)
		if err != nil {
			return nil, err
		}
		if cat == nil || cat.RestaurantID != item.RestaurantID {
			return nil, errors.ErrInvalidArgument
		}
		item.CategoryID = *in.CategoryID
	}
	item.UpdatedAt = time.Now()
	if err := s.repo.UpdateItem(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *MenuService) DeleteItem(ctx context.Context, id uuid.UUID) error {
	item, err := s.repo.GetItemByID(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		return errors.ErrNotFound
	}
	return s.repo.DeleteItem(ctx, id)
}
