package service

import (
	"context"
	"errors"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	cart "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	menu "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	"github.com/google/uuid"
	"testing"
)

type menuStub struct {
	item *menu.MenuItem
	err  error
}

func (m menuStub) GetItemByID(context.Context, uuid.UUID) (*menu.MenuItem, error) {
	return m.item, m.err
}

type mutationRepo struct {
	repository.CartRepository
	called bool
	err    error
}

func (r *mutationRepo) AddToCart(_ context.Context, user string, rid uuid.UUID, item cart.CartItem) (*cart.Cart, error) {
	r.called = true
	if user != "user" || item.UnitPrice != 54900 || item.Name != "Dish" || item.Quantity != 2 {
		panic("wrong cart snapshot")
	}
	return nil, r.err
}
func TestAddItemGuardsAndPersistenceErrors(t *testing.T) {
	rid := uuid.New()
	dbError := errors.New("storage unavailable")
	for _, tc := range []struct {
		name     string
		menu     menuStub
		quantity int
		want     error
		called   bool
	}{
		{name: "bad quantity", quantity: 0, want: coreerrors.ErrInvalidArgument},
		{name: "missing item", quantity: 2, want: coreerrors.ErrNotFound},
		{name: "menu failure", menu: menuStub{err: dbError}, quantity: 2, want: dbError},
		{name: "unavailable item", menu: menuStub{item: &menu.MenuItem{RestaurantID: rid}}, quantity: 2, want: coreerrors.ErrConflict},
		{name: "wrong restaurant", menu: menuStub{item: &menu.MenuItem{RestaurantID: uuid.New(), IsAvailable: true}}, quantity: 2, want: coreerrors.ErrConflict},
		{name: "persistence failure", menu: menuStub{item: &menu.MenuItem{RestaurantID: rid, IsAvailable: true, Name: "Dish", Price: 54900}}, quantity: 2, want: dbError, called: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mutationRepo{err: dbError}
			svc := NewCartService(repo, tc.menu)
			_, err := svc.AddItem(context.Background(), "user", rid, uuid.New(), tc.quantity)
			if !errors.Is(err, tc.want) || repo.called != tc.called {
				t.Fatalf("error=%v mutation=%t", err, repo.called)
			}
		})
	}
}

func (r *mutationRepo) SetQuantity(context.Context, string, uuid.UUID, int) (*cart.Cart, error) {
	return nil, r.err
}
func (r *mutationRepo) RemoveCartItem(context.Context, string, uuid.UUID) (*cart.Cart, error) {
	return nil, r.err
}
func (r *mutationRepo) ClearByUser(context.Context, string) error               { return r.err }
func (r *mutationRepo) GetByUserID(context.Context, string) (*cart.Cart, error) { return nil, r.err }
func TestCartServiceDoesNotSwallowMutationFailures(t *testing.T) {
	failure := errors.New("storage failed")
	svc := NewCartService(&mutationRepo{err: failure}, nil)
	if _, err := svc.GetCart(context.Background(), "user"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := svc.UpdateItemQuantity(context.Background(), "user", uuid.New(), 2); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := svc.RemoveItem(context.Background(), "user", uuid.New()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := svc.ClearCart(context.Background(), "user"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, qty := range []int{0, 51} {
		if _, err := svc.UpdateItemQuantity(context.Background(), "user", uuid.New(), qty); !errors.Is(err, coreerrors.ErrInvalidArgument) {
			t.Fatal(err)
		}
	}
}
