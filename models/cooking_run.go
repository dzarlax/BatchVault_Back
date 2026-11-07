package models

import (
	"encoding/json"
	"time"
)

type CookingRun struct {
	ID              uint                `json:"id" gorm:"primaryKey"`
	WorkspaceID     uint                `json:"-" gorm:"not null;index"`
	UserID          uint                `json:"-" gorm:"not null"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	Date            time.Time           `json:"date"`
	Title           string              `json:"title"`
	State           string              `json:"state" gorm:"not null;default:draft"`
	Version         uint                `json:"version" gorm:"not null;default:1"`
	SourceSignature string              `json:"source_signature"`
	Plan            CookingPlan         `json:"plan" gorm:"serializer:json;type:jsonb"`
	Preparation     *CookingPreparation `json:"preparation" gorm:"serializer:json;type:jsonb"`
	ActualOutput    []CookingComponent  `json:"actual_output" gorm:"serializer:json;type:jsonb"`
	PlanInput       CookingPlanInput    `json:"plan_input" gorm:"serializer:json;type:jsonb"`
	Events          []CookingEvent      `json:"events" gorm:"-"`
	Lots            []CookingStockLot   `json:"lots" gorm:"-"`
	StockVersion    uint                `json:"stock_version" gorm:"-"`
	OrderChanges    []CookingIssue      `json:"order_changes,omitempty" gorm:"-"`
}

type CookingRunFlavor struct {
	ID             uint                  `gorm:"primaryKey"`
	WorkspaceID    uint                  `gorm:"not null;index"`
	RunID          uint                  `gorm:"not null;uniqueIndex:cooking_run_recipe"`
	RecipeID       uint                  `gorm:"not null;uniqueIndex:cooking_run_recipe"`
	ActualMeatG    string                `gorm:"type:numeric(14,3);not null;default:0"`
	RecipeSnapshot CookingRecipeSnapshot `gorm:"serializer:json;type:jsonb"`
}
type CookingRunOrder struct {
	ID          uint `gorm:"primaryKey"`
	WorkspaceID uint `gorm:"not null;index"`
	RunID       uint `gorm:"not null;uniqueIndex:cooking_run_order_item"`
	OrderID     uint `gorm:"not null;index"`
	OrderItemID uint `gorm:"not null;uniqueIndex:cooking_run_order_item"`
	Quantity    int  `gorm:"not null"`
}
type CookingEvent struct {
	ID          uint            `json:"id" gorm:"primaryKey"`
	WorkspaceID uint            `json:"-" gorm:"not null;uniqueIndex:cooking_operation"`
	RunID       uint            `json:"run_id" gorm:"not null;index"`
	UserID      uint            `json:"-" gorm:"not null"`
	OperationID string          `json:"operation_id" gorm:"not null;uniqueIndex:cooking_operation"`
	RequestHash string          `json:"-"`
	Kind        string          `json:"kind"`
	CreatedAt   time.Time       `json:"created_at"`
	Payload     json.RawMessage `json:"payload" gorm:"type:jsonb" swaggertype:"object"`
	Result      json.RawMessage `json:"-" gorm:"type:jsonb"`
}
type CookingPlanInput struct {
	RunID              *uint               `json:"run_id,omitempty"`
	OrderIDs           []uint              `json:"order_ids"`
	Extras             []CookingExtraInput `json:"extras"`
	BaseSelections     map[uint]uint       `json:"base_selections,omitempty"`
	StockUses          []CookingStockUse   `json:"stock_uses,omitempty"`
	AcknowledgedRunIDs []uint              `json:"acknowledged_run_ids,omitempty"`
}
type CookingStockUse struct {
	LotID    uint   `json:"lot_id"`
	Quantity string `json:"quantity"`
}
type CookingPlanPreview struct {
	Plan            CookingPlan        `json:"plan"`
	SourceSignature string             `json:"source_signature"`
	StockVersion    uint               `json:"stock_version"`
	ActiveRuns      []CookingActiveRun `json:"active_runs"`
	Stock           CookingStock       `json:"stock"`
}
type CookingActiveRun struct {
	ID    uint   `json:"id"`
	Title string `json:"title"`
}
type CookingCreateInput struct {
	Version uint `json:"version,omitempty"`
	CookingPlanInput
	Title           string `json:"title"`
	Date            string `json:"date"`
	SourceSignature string `json:"source_signature"`
	StockVersion    uint   `json:"stock_version"`
	OperationID     string `json:"operation_id"`
}
type CookingPackingInput struct {
	Version       uint                      `json:"version"`
	StockVersion  uint                      `json:"stock_version"`
	OperationID   string                    `json:"operation_id"`
	Lines         []CookingPackingLineInput `json:"lines"`
	Sources       []CookingStockUse         `json:"sources,omitempty"`
	ActualUsableG []CookingComponent        `json:"actual_usable_g,omitempty"`
}
type CookingResultInput struct {
	Version       uint                 `json:"version"`
	StockVersion  uint                 `json:"stock_version"`
	OperationID   string               `json:"operation_id"`
	ActualUsableG []CookingComponent   `json:"actual_usable_g"`
	Packing       *CookingPackingInput `json:"packing,omitempty"`
}
type CookingArchiveInput struct {
	Version     uint   `json:"version"`
	OperationID string `json:"operation_id"`
}
