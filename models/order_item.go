package models

import (
	"time"

	"gorm.io/gorm"
)

// OrderItem represents order items model
type OrderItem struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"index" swaggerignore:"true"`
	OrderID      uint           `json:"order_id"`
	ProductID    uint           `json:"product_id"`
	Quantity     int            `json:"quantity" gorm:"not null"`
	Price        float64        `json:"price" gorm:"not null"`
	Cost_price   float64        `json:"cost_price"`
	CostSnapshot *CostSnapshot  `json:"cost_snapshot" gorm:"serializer:json;type:jsonb"`
	Order        Order          `json:"order" gorm:"foreignKey:OrderID"`
	Product      Product        `json:"product" gorm:"foreignKey:ProductID"`
}
