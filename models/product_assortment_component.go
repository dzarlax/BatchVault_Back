package models

import (
	"gorm.io/gorm"
	"time"
)

type ProductAssortmentComponent struct {
	ID                 uint           `json:"-" gorm:"primaryKey"`
	CreatedAt          time.Time      `json:"-"`
	UpdatedAt          time.Time      `json:"-"`
	DeletedAt          gorm.DeletedAt `json:"-" gorm:"index"`
	WorkspaceID        uint           `json:"-" gorm:"not null;index"`
	ProductID          uint           `json:"-" gorm:"not null;index"`
	ComponentProduct   *Product       `json:"-" gorm:"foreignKey:ComponentProductID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	ComponentProductID uint           `json:"component_product_id" gorm:"not null"`
	QuantityG          Decimal        `json:"quantity_g" gorm:"type:numeric(14,3);not null;check:assortment_component_quantity_positive,quantity_g > 0"`
	Position           int            `json:"position"`
}
