package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

const (
	minDeviceTokenLength = 32
	maxDeviceTokenLength = 512
)

type pushDeviceRequest struct {
	DeviceToken string `json:"device_token" binding:"required"`
	Platform    string `json:"platform" binding:"required,oneof=ios"`
	Environment string `json:"environment" binding:"required,oneof=development production"`
}

type pushDeviceDeleteRequest struct {
	DeviceToken string `json:"device_token" binding:"required"`
}

// UpsertCurrentPushDevice registers the current APNs token for the authenticated user.
func UpsertCurrentPushDevice(c *gin.Context) {
	userID := c.MustGet("userID").(uint)

	var request pushDeviceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid push device registration"})
		return
	}

	request.DeviceToken = strings.TrimSpace(request.DeviceToken)
	if !validDeviceToken(request.DeviceToken) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device_token"})
		return
	}

	now := time.Now().UTC()
	device := models.PushDevice{
		UserID:      userID,
		DeviceToken: request.DeviceToken,
		Platform:    request.Platform,
		Environment: request.Environment,
		Enabled:     true,
		LastSeenAt:  now,
	}
	if err := pushDeviceDB().Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_token"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"user_id":      userID,
			"platform":     request.Platform,
			"environment":  request.Environment,
			"enabled":      true,
			"last_seen_at": now,
			"updated_at":   now,
		}),
	}).Create(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register push device"})
		return
	}

	c.Status(http.StatusNoContent)
}

// DisableCurrentPushDevice disables the current APNs token for the authenticated user.
func DisableCurrentPushDevice(c *gin.Context) {
	userID := c.MustGet("userID").(uint)

	var request pushDeviceDeleteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid push device request"})
		return
	}

	deviceToken := strings.TrimSpace(request.DeviceToken)
	if !validDeviceToken(deviceToken) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device_token"})
		return
	}

	if err := pushDeviceDB().Model(&models.PushDevice{}).
		Where("user_id = ? AND device_token = ?", userID, deviceToken).
		Updates(map[string]interface{}{"enabled": false, "updated_at": time.Now().UTC()}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable push device"})
		return
	}

	c.Status(http.StatusNoContent)
}

func pushDeviceDB() *gorm.DB {
	return database.DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
}

func validDeviceToken(deviceToken string) bool {
	return len(deviceToken) >= minDeviceTokenLength && len(deviceToken) <= maxDeviceTokenLength
}
