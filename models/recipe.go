package models

import (
	"time"

	"gorm.io/gorm"
)

// RecipeCreateDTO represents data for creating a new recipe (without nested User)
type RecipeCreateDTO struct {
	Name          string          `json:"name" binding:"required,min=1"`
	PlannedYieldG OptionalDecimal `json:"planned_yield_g" swaggertype:"number"`
}

// Recipe represents recipe model
type Recipe struct {
	ID                uint               `json:"id" gorm:"primaryKey"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	DeletedAt         gorm.DeletedAt     `json:"deleted_at,omitempty" gorm:"index" swaggerignore:"true"`
	Name              string             `json:"name" gorm:"not null" binding:"required,min=1"`
	PlannedYieldG     *Decimal           `json:"planned_yield_g" gorm:"type:numeric(14,3);check:recipe_yield_positive,planned_yield_g IS NULL OR planned_yield_g > 0" swaggertype:"number"`
	CostEstimate      *CostEstimate      `json:"cost_estimate,omitempty" gorm:"-"`
	UserID            uint               `json:"user_id" gorm:"not null"`
	WorkspaceID       *uint              `json:"workspace_id,omitempty"`
	User              User               `json:"user" gorm:"foreignKey:UserID"`
	Workspace         Workspace          `json:"workspace" gorm:"foreignKey:WorkspaceID"`
	RecipeIngredients []RecipeIngredient `json:"recipe_ingredients" gorm:"foreignKey:RecipeID"`
	CookingSessions   []CookingSession   `json:"cooking_sessions" gorm:"foreignKey:RecipeID"`
	ProductOptions    []ProductOption    `json:"product_options" gorm:"foreignKey:RecipeID"`
	TotalCost         float64            `json:"total_cost" gorm:"-"` // Field not persisted to database
}
