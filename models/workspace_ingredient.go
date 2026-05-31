package models

import (
	"time"

	"gorm.io/gorm"
)

// WorkspaceIngredient represents an ingredient in a workspace working set.
type WorkspaceIngredient struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index" swaggerignore:"true"`
	WorkspaceID  uint           `json:"workspace_id" gorm:"not null"`
	IngredientID uint           `json:"ingredient_id" gorm:"not null"`
	Active       bool           `json:"active" gorm:"not null;default:true"`
	Alias        string         `json:"alias"`
	Category     string         `json:"category"`
	Workspace    Workspace      `json:"workspace" gorm:"foreignKey:WorkspaceID"`
	Ingredient   Ingredient     `json:"ingredient" gorm:"foreignKey:IngredientID"`
	LatestPrice  *Price         `json:"latest_price,omitempty" gorm:"-"`
	UnitProfile  string         `json:"unit_profile" gorm:"-"`
	DefaultUnit  string         `json:"default_unit" gorm:"-"`
	AllowedUnits []string       `json:"allowed_units" gorm:"-"`
}

// WorkspaceIngredientCreateDTO represents data for linking an ingredient to a workspace.
type WorkspaceIngredientCreateDTO struct {
	IngredientID uint `json:"ingredient_id" binding:"required"`
}

// WorkspaceIngredientUpdateDTO represents editable workspace ingredient metadata.
type WorkspaceIngredientUpdateDTO struct {
	Active   *bool   `json:"active"`
	Alias    *string `json:"alias"`
	Category *string `json:"category"`
}
