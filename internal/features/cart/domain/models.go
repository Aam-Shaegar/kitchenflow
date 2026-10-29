package domain

import (
	"time"

	"github.com/google/uuid"
)

type Cart struct {
	ID           uuid.UUID  `json:"id"`
	UserID       string     `json:"user_id"`
	RestaurantID uuid.UUID  `json:"restaurant_id"`
	Items        []CartItem `json:"items"`
	// TotalAmount is kept in minor units (kopecks) as int64, never float.
	TotalAmount int64     `json:"total_amount"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CartItem struct {
	ID         uuid.UUID `json:"id"`
	CartID     uuid.UUID `json:"cart_id"`
	MenuItemID uuid.UUID `json:"menu_item_id"`
	Name       string    `json:"name"`
	// UnitPrice is a frozen snapshot in minor units (kopecks); history must survive menu edits.
	UnitPrice int64 `json:"unit_price"`
	Quantity  int   `json:"quantity"`
	// TotalPrice is kept in minor units (kopecks) as int64, never float.
	TotalPrice int64     `json:"total_price"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (c *Cart) CalculateTotal() int64 {
	total := int64(0)
	for _, item := range c.Items {
		total += item.TotalPrice
	}
	return total
}

func (c *Cart) FindItemIndex(menuItemID uuid.UUID) int {
	for i := range c.Items {
		if c.Items[i].MenuItemID == menuItemID {
			return i
		}
	}
	return -1
}
