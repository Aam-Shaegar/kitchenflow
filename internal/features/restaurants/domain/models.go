package domain

import (
	"time"

	"github.com/google/uuid"
)

type Restaurant struct {
	ID                 uuid.UUID `json:"id"`
	ExternalID         string    `json:"external_id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	IsOpen             bool      `json:"is_open"`
	IntegrationBaseURL string    `json:"integration_base_url"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type RestaurantInfo struct {
	IntegrationBaseURL string    `json:"-"`
	ID                 uuid.UUID `json:"id"`
	ExternalID         string    `json:"external_id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	IsOpen             bool      `json:"is_open"`
}

type RestaurantListItem struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsOpen      bool      `json:"is_open"`
}
