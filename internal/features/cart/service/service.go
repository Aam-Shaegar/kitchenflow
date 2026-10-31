package service

import (
	"context"

	"github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	cart_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	menu_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	"github.com/google/uuid"
)

type MenuProvider interface {
	GetItemByID(ctx context.Context, id uuid.UUID) (*menu_domain.MenuItem, error)
}

type CartService struct {
	repo         repository.CartRepository
	menuProvider MenuProvider
}

func NewCartService(repo repository.CartRepository, menuProvider MenuProvider) *CartService {
	return &CartService{repo: repo, menuProvider: menuProvider}
}

func (s *CartService) GetCart(ctx context.Context, userID string) (*cart_domain.Cart, error) {
	cart, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cart == nil {
		return nil, errors.ErrNotFound
	}
	return cart, nil
}

func (s *CartService) AddItem(ctx context.Context, userID string, restaurantID, menuItemID uuid.UUID, quantity int) (*cart_domain.Cart, error) {
	if quantity <= 0 || quantity > 50 {
		return nil, errors.ErrInvalidArgument
	}

	menuItem, err := s.menuProvider.GetItemByID(ctx, menuItemID)
	if err != nil {
		return nil, err
	}
	if menuItem == nil {
		return nil, errors.ErrNotFound
	}

	if !menuItem.IsAvailable {
		return nil, errors.ErrConflict
	}

	if menuItem.RestaurantID != restaurantID {
		return nil, errors.ErrConflict
	}

	return s.repo.AddToCart(ctx, userID, restaurantID, cart_domain.CartItem{MenuItemID: menuItemID, Name: menuItem.Name, UnitPrice: menuItem.Price, Quantity: quantity})
}
func (s *CartService) UpdateItemQuantity(ctx context.Context, userID string, id uuid.UUID, quantity int) (*cart_domain.Cart, error) {
	if quantity < 1 || quantity > 50 {
		return nil, errors.ErrInvalidArgument
	}
	return s.repo.SetQuantity(ctx, userID, id, quantity)
}
func (s *CartService) RemoveItem(ctx context.Context, userID string, id uuid.UUID) (*cart_domain.Cart, error) {
	return s.repo.RemoveCartItem(ctx, userID, id)
}
func (s *CartService) ClearCart(ctx context.Context, userID string) error {
	return s.repo.ClearByUser(ctx, userID)
}
