package controllers

import (
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

const (
	minDeviceTokenLength        = 32
	maxDeviceTokenLength        = 512
	maxActivePushDevicesPerUser = 10
)

type pushDeviceRequest struct {
	DeviceToken    string `json:"device_token" binding:"required"`
	InstallationID string `json:"installation_id" binding:"required"`
	Platform       string `json:"platform" binding:"required,oneof=ios"`
	Environment    string `json:"environment" binding:"required,oneof=development production"`
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

	deviceToken, ok := canonicalDeviceToken(request.DeviceToken)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device_token"})
		return
	}
	request.DeviceToken = deviceToken
	installationID, ok := canonicalInstallationID(request.InstallationID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid installation_id"})
		return
	}

	now := time.Now().UTC()
	if err := pushDeviceDB().Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND installation_id = ? AND device_token <> ?", userID, installationID, request.DeviceToken).Delete(&models.PushDevice{}).Error; err != nil {
			return err
		}

		device := models.PushDevice{
			UserID:         userID,
			InstallationID: installationID,
			DeviceToken:    request.DeviceToken,
			Platform:       request.Platform,
			Environment:    request.Environment,
			Enabled:        true,
			LastSeenAt:     now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_token"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"user_id":         userID,
				"installation_id": installationID,
				"platform":        request.Platform,
				"environment":     request.Environment,
				"enabled":         true,
				"last_seen_at":    now,
				"updated_at":      now,
			}),
		}).Create(&device).Error; err != nil {
			return err
		}
		return enforcePushDeviceLimit(tx, userID)
	}); err != nil {
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

	deviceToken, ok := canonicalDeviceToken(request.DeviceToken)
	if !ok {
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

func canonicalDeviceToken(deviceToken string) (string, bool) {
	canonicalToken := strings.ToLower(strings.TrimSpace(deviceToken))
	if len(canonicalToken) < minDeviceTokenLength || len(canonicalToken) > maxDeviceTokenLength || len(canonicalToken)%2 != 0 {
		return "", false
	}
	if _, err := hex.DecodeString(canonicalToken); err != nil {
		return "", false
	}
	return canonicalToken, true
}

func canonicalInstallationID(installationID string) (string, bool) {
	parsedID, err := uuid.Parse(strings.TrimSpace(installationID))
	if err != nil {
		return "", false
	}
	return strings.ToLower(parsedID.String()), true
}

func enforcePushDeviceLimit(db *gorm.DB, userID uint) error {
	var excessDevices []models.PushDevice
	if err := db.Where("user_id = ? AND enabled = ?", userID, true).
		Order("last_seen_at DESC, id DESC").
		Offset(maxActivePushDevicesPerUser).
		Find(&excessDevices).Error; err != nil {
		return err
	}
	if len(excessDevices) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(excessDevices))
	for _, device := range excessDevices {
		ids = append(ids, device.ID)
	}
	return db.Model(&models.PushDevice{}).Where("id IN ?", ids).Updates(map[string]interface{}{"enabled": false, "updated_at": time.Now().UTC()}).Error
}
