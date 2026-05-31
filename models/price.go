package models

import (
	"time"

	"gorm.io/gorm"
)

// PriceCreateDTO represents data for creating a new price (without nested Ingredient)
type PriceCreateDTO struct {
	IngredientID uint      `json:"ingredient_id" binding:"required"`
	Price        float64   `json:"price" binding:"required,min=0"`
	Quantity     float64   `json:"quantity" binding:"required,gt=0"`
	Unit         string    `json:"unit"`
	Date         time.Time `json:"date"`
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
