package models

import (
	"time"

	"gorm.io/gorm"
)

// PackageCreateDTO represents data for creating a new package (without nested User and Products)
type PackageCreateDTO struct {
	Name       string          `json:"name" binding:"required,min=1"`
	NetWeightG OptionalDecimal `json:"net_weight_g" swaggertype:"number"`
	UnitCost   OptionalDecimal `json:"unit_cost" swaggertype:"number"`
}

// Package represents packaging model
type Package struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index" swaggerignore:"true"`
	Name        string         `json:"name" gorm:"not null" binding:"required,min=1"`
	NetWeightG  *Decimal       `json:"net_weight_g" gorm:"type:numeric(14,3);check:package_weight_positive,net_weight_g IS NULL OR net_weight_g > 0" swaggertype:"number"`
	UnitCost    *Decimal       `json:"unit_cost" gorm:"type:numeric(14,2);check:package_cost_nonnegative,unit_cost IS NULL OR unit_cost >= 0" swaggertype:"number"`
	UserID      uint           `json:"user_id"`
	WorkspaceID *uint          `json:"workspace_id,omitempty"`
	User        User           `json:"user" gorm:"foreignKey:UserID"`
	Workspace   Workspace      `json:"workspace" gorm:"foreignKey:WorkspaceID"`
	Products    []Product      `json:"products" gorm:"foreignKey:PackageID"` // If Package is related to Product through PackageID
}
