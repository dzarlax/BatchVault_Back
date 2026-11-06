package models

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// PriceCreateDTO represents data for creating a new price (without nested Ingredient)
type PriceCreateDTO struct {
	IngredientID uint      `json:"ingredient_id" binding:"required"`
	Price        float64   `json:"price" binding:"min=0"`
	Quantity     float64   `json:"quantity" binding:"required,gt=0"`
	Unit         string    `json:"unit"`
	Date         time.Time `json:"date"`
}

// UnmarshalJSON requires an explicit price while permitting a genuinely free ingredient.
func (p *PriceCreateDTO) UnmarshalJSON(data []byte) error {
	type plain PriceCreateDTO
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	price, exists := fields["price"]
	if !exists || string(price) == "null" {
		return fmt.Errorf("price is required and cannot be null")
	}
	return json.Unmarshal(data, (*plain)(p))
}

// Price represents ingredient price model
type Price struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index" swaggerignore:"true"`
	IngredientID uint           `json:"ingredient_id" binding:"required"`
	Price        float64        `json:"price" gorm:"not null" binding:"required,min=0"`
	Unit         string         `json:"unit"`
	Quantity     float64        `json:"quantity" binding:"gt=0"`
	Date         time.Time      `json:"date" gorm:"not null" binding:"required"`
	UserID       uint           `json:"user_id"`
	WorkspaceID  *uint          `json:"workspace_id,omitempty"`
	User         User           `json:"user" gorm:"foreignKey:UserID"`
	Workspace    Workspace      `json:"workspace" gorm:"foreignKey:WorkspaceID"`
	Ingredient   Ingredient     `json:"ingredient" gorm:"foreignKey:IngredientID"`
}
