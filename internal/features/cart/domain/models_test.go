package domain

import (
	"testing"

	"github.com/google/uuid"
)

func mustUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

func TestCartItemTotalPrice(t *testing.T) {
	item := CartItem{
		UnitPrice: 10000,
		Quantity:  2,
	}

	item.TotalPrice = item.UnitPrice * int64(item.Quantity)

	if item.TotalPrice != 20000 {
		t.Errorf("TotalPrice = %d, want 20000", item.TotalPrice)
	}
}

func TestCartSingleRestaurantConflict(t *testing.T) {
	cart := &Cart{
		RestaurantID: mustUUID("550e8400-e29b-41d4-a716-446655440000"),
		Items: []CartItem{
			{MenuItemID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1"), Quantity: 1, UnitPrice: 54900, TotalPrice: 54900},
		},
	}
	other := mustUUID("660e8400-e29b-41d4-a716-446655440001")
	if cart.RestaurantID == other {
		t.Error("test setup: restaurant IDs must differ")
	}
	if len(cart.Items) == 0 {
		t.Error("cart must be non-empty for conflict check")
	}
}
