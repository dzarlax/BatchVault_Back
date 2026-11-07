package models

import "time"

type CookingWorkspaceState struct {
	WorkspaceID uint `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Version     uint `json:"version" gorm:"not null;default:1"`
}
type CookingStockLot struct {
	ID                   uint               `json:"id" gorm:"primaryKey"`
	WorkspaceID          uint               `json:"-" gorm:"not null;index"`
	RunID                uint               `json:"run_id" gorm:"not null;index"`
	Kind                 string             `json:"kind" gorm:"not null"`
	RecipeID             uint               `json:"recipe_id,omitempty" gorm:"index"`
	ProductID            uint               `json:"product_id,omitempty" gorm:"index"`
	Name                 string             `json:"name"`
	Quantity             string             `json:"quantity" gorm:"type:numeric(14,3);not null;check:cooking_quantity_positive,quantity >= 0"`
	Reserved             string             `json:"reserved" gorm:"type:numeric(14,3);not null;default:0;check:cooking_reserved_valid,reserved >= 0 AND reserved <= quantity"`
	Available            string             `json:"available" gorm:"-"`
	Unit                 string             `json:"unit"`
	Composition          []CookingComponent `json:"composition" gorm:"serializer:json;type:jsonb"`
	ConfigurationVersion uint               `json:"configuration_version"`
	CreatedAt            time.Time          `json:"created_at"`
}
type CookingStockAllocation struct {
	ID          uint   `gorm:"primaryKey"`
	WorkspaceID uint   `gorm:"not null;index"`
	RunID       uint   `gorm:"not null;index"`
	LotID       uint   `gorm:"not null;index"`
	OrderID     uint   `gorm:"not null;default:0;index"`
	OrderItemID uint   `gorm:"not null;default:0;index"`
	Quantity    string `gorm:"type:numeric(14,3);not null"`
	Kind        string `gorm:"not null"`
	State       string `gorm:"not null"`
	CreatedAt   time.Time
}
type CookingStock struct {
	Events  []CookingEvent    `json:"events"`
	Version uint              `json:"version"`
	Lots    []CookingStockLot `json:"lots"`
}
type CookingAdjustmentInput struct {
	OperationID  string `json:"operation_id"`
	StockVersion uint   `json:"stock_version"`
	Kind         string `json:"kind"`
	Reason       string `json:"reason"`
	LotID        uint   `json:"lot_id,omitempty"`
	RecipeID     uint   `json:"recipe_id,omitempty"`
	ProductID    uint   `json:"product_id,omitempty"`
	Quantity     string `json:"quantity"`
	RunID        uint   `json:"run_id,omitempty"`
}
