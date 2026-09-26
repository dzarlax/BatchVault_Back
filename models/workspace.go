package models

import (
	"time"

	"gorm.io/gorm"
)

// Workspace represents an operational data boundary.
type Workspace struct {
	ID             uint                  `json:"id" gorm:"primaryKey"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	DeletedAt      gorm.DeletedAt        `json:"deleted_at,omitempty" gorm:"index" swaggerignore:"true"`
	Name           string                `json:"name" gorm:"not null" binding:"required,min=1"`
	Slug           string                `json:"slug" gorm:"not null" binding:"required,min=1"`
	Currency       string                `json:"currency" gorm:"not null;default:RSD"`
	AccountID      *uint                 `json:"account_id,omitempty"`
	PersonalUserID *uint                 `json:"-"`
	Members        []WorkspaceMember     `json:"members,omitempty" gorm:"foreignKey:WorkspaceID"`
	Ingredients    []WorkspaceIngredient `json:"ingredients,omitempty" gorm:"foreignKey:WorkspaceID"`
}
