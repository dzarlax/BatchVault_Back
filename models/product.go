package models

import (
	"time"

	"gorm.io/gorm"
)

type Product struct {
	ID             uint                   `json:"id" gorm:"primaryKey"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	DeletedAt      gorm.DeletedAt         `json:"deleted_at,omitempty" gorm:"index" swaggerignore:"true"`
	Name           string                 `json:"name" gorm:"not null" binding:"required,min=1"`
	Description    string                 `json:"description"`
	Price          float64                `json:"price" gorm:"not null" binding:"required,min=0"`
	Cost           float64                `json:"cost" gorm:"not null" binding:"min=0"`
	CostMode       string                 `json:"cost_mode" gorm:"not null;default:manual;check:product_cost_mode_valid,cost_mode IN ('manual','recipe','assortment')"`
	CostRecipeID   *uint                  `json:"cost_recipe_id"`
	CostComponents []ProductCostComponent `json:"components" gorm:"foreignKey:ProductID"`
	CostEstimate   *CostEstimate          `json:"cost_estimate,omitempty" gorm:"-"`
	Image          string                 `json:"image"`
	UserID         uint                   `json:"user_id"`
	WorkspaceID    *uint                  `json:"workspace_id,omitempty"`
	PackageID      uint                   `json:"package_id"` // Add this field
	User           User                   `json:"user" gorm:"foreignKey:UserID"`
	Workspace      Workspace              `json:"workspace" gorm:"foreignKey:WorkspaceID"`
	Options        []ProductOption        `json:"options" gorm:"foreignKey:ProductID"`
	Package        Package                `json:"package" gorm:"foreignKey:PackageID"` // Add this field if you need to load package data
}
