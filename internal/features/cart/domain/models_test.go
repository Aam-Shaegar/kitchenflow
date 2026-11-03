package domain

import (
	"github.com/google/uuid"
	"testing"
)

func TestCartTotalsAndItemLookup(t *testing.T) {
	id, other := uuid.New(), uuid.New()
	cart := Cart{Items: []CartItem{{MenuItemID: id, TotalPrice: 109800}, {MenuItemID: other, TotalPrice: 12345}}}
	if cart.CalculateTotal() != 122145 {
		t.Fatal("cart total ignores a line")
	}
	if cart.FindItemIndex(id) != 0 || cart.FindItemIndex(other) != 1 || cart.FindItemIndex(uuid.New()) != -1 {
		t.Fatal("item lookup is incorrect")
	}
	empty := Cart{}
	if empty.CalculateTotal() != 0 || empty.FindItemIndex(id) != -1 {
		t.Fatal("empty cart result incorrect")
	}
}
