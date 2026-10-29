package domain

import (
	"time"

	"github.com/google/uuid"
)

type MenuCategory struct {
	ID           uuid.UUID  `json:"id"`
	RestaurantID uuid.UUID  `json:"restaurant_id"`
	Name         string     `json:"name"`
	Position     int        `json:"position"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Items        []MenuItem `json:"items"`
}

type MenuItem struct {
	ID           uuid.UUID `json:"id"`
	RestaurantID uuid.UUID `json:"restaurant_id"`
	CategoryID   uuid.UUID `json:"category_id"`
	ExternalID   string    `json:"external_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	// Price is kept in minor units (kopecks) as int64, never float.
	Price       int64     `json:"price"`
	IsAvailable bool      `json:"is_available"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Menu struct {
	RestaurantID uuid.UUID      `json:"restaurant_id"`
	Categories   []MenuCategory `json:"categories"`
}
