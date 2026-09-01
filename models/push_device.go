package models

import (
	"time"
)

// PushDevice stores an APNs device registration for a user account.
type PushDevice struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	UserID         uint      `json:"user_id" gorm:"not null;index;uniqueIndex:idx_push_devices_user_installation"`
	InstallationID string    `json:"-" gorm:"not null;uniqueIndex:idx_push_devices_user_installation" swaggerignore:"true"`
	DeviceToken    string    `json:"-" gorm:"not null;uniqueIndex" swaggerignore:"true"`
	Platform       string    `json:"platform" gorm:"not null"`
	Environment    string    `json:"environment" gorm:"not null"`
	Enabled        bool      `json:"enabled" gorm:"not null;default:true;index"`
	LastSeenAt     time.Time `json:"last_seen_at" gorm:"not null"`
}
