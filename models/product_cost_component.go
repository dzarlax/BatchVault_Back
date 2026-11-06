package models

import (
	"gorm.io/gorm"
	"time"
)

type ProductCostComponent struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index" swaggerignore:"true"`
	WorkspaceID uint           `json:"workspace_id" gorm:"not null;index"`
	ProductID   uint           `json:"product_id" gorm:"not null;index"`
	RecipeID    uint           `json:"recipe_id" gorm:"not null"`
	QuantityG   Decimal        `json:"quantity_g" swaggertype:"number" gorm:"type:numeric(14,3);not null;check:product_component_quantity_positive,quantity_g > 0"`
	Position    int            `json:"position"`
}
