package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

func setupPushDeviceTest(t *testing.T) models.User {
	t.Helper()

	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&models.User{}, &models.PushDevice{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	database.DB = db

	user := models.User{Username: "push-device-user", Password: "hashed"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func runPushDeviceRequest(userID uint, handler gin.HandlerFunc, method string, body interface{}) *httptest.ResponseRecorder {
	payload, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	router := gin.New()
	router.Handle(method, "/push-devices/current", func(c *gin.Context) {
		c.Set("userID", userID)
		handler(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/push-devices/current", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestUpsertCurrentPushDeviceKeepsOneTokenAndTransfersCurrentUser(t *testing.T) {
	user := setupPushDeviceTest(t)
	secondUser := models.User{Username: "second-push-device-user", Password: "hashed"}
	if err := database.DB.Create(&secondUser).Error; err != nil {
		t.Fatalf("create second user: %v", err)
	}

	firstResponse := runPushDeviceRequest(user.ID, UpsertCurrentPushDevice, http.MethodPut, gin.H{
		"device_token":    "ABCDEF0123456789ABCDEF0123456789",
		"installation_id": "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
		"platform":        "ios",
		"environment":     "development",
	})
	if firstResponse.Code != http.StatusNoContent {
		t.Fatalf("first registration status = %d body = %s", firstResponse.Code, firstResponse.Body.String())
	}

	secondResponse := runPushDeviceRequest(secondUser.ID, UpsertCurrentPushDevice, http.MethodPut, gin.H{
		"device_token":    "abcdef0123456789abcdef0123456789",
		"installation_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"platform":        "ios",
		"environment":     "production",
	})
	if secondResponse.Code != http.StatusNoContent {
		t.Fatalf("second registration status = %d body = %s", secondResponse.Code, secondResponse.Body.String())
	}

	var devices []models.PushDevice
	if err := database.DB.Find(&devices).Error; err != nil {
		t.Fatalf("load push devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("push device count = %d, want 1", len(devices))
	}
	if devices[0].UserID != secondUser.ID || devices[0].DeviceToken != "abcdef0123456789abcdef0123456789" || devices[0].Environment != "production" || !devices[0].Enabled {
		t.Fatalf("upsert did not replace the device registration")
	}
}

func TestDisableCurrentPushDeviceOnlyAffectsTheAuthenticatedUser(t *testing.T) {
	user := setupPushDeviceTest(t)
	otherUser := models.User{Username: "other-push-device-user", Password: "hashed"}
	if err := database.DB.Create(&otherUser).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}
	devices := []models.PushDevice{
		{UserID: user.ID, DeviceToken: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Platform: "ios", Environment: "development", Enabled: true},
		{UserID: otherUser.ID, DeviceToken: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Platform: "ios", Environment: "development", Enabled: true},
	}
	if err := database.DB.Create(&devices).Error; err != nil {
		t.Fatalf("create push devices: %v", err)
	}

	response := runPushDeviceRequest(user.ID, DisableCurrentPushDevice, http.MethodDelete, gin.H{"device_token": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	if response.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d body = %s", response.Code, response.Body.String())
	}

	var ownerDevice, otherDevice models.PushDevice
	if err := database.DB.Where("device_token = ?", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").First(&ownerDevice).Error; err != nil {
		t.Fatalf("load owner device: %v", err)
	}
	if err := database.DB.Where("device_token = ?", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").First(&otherDevice).Error; err != nil {
		t.Fatalf("load other device: %v", err)
	}
	if !ownerDevice.Enabled || !otherDevice.Enabled {
		t.Fatalf("a device belonging to another user was disabled")
	}
}

func TestPushDeviceTokenValidationIsBounded(t *testing.T) {
	if _, ok := canonicalDeviceToken(""); ok {
		t.Fatalf("empty device token was accepted")
	}
	if _, ok := canonicalDeviceToken("short-device-token"); ok {
		t.Fatalf("short device token was accepted")
	}
	if _, ok := canonicalDeviceToken(string(bytes.Repeat([]byte("a"), minDeviceTokenLength))); !ok {
		t.Fatalf("minimum-length device token was rejected")
	}
	if _, ok := canonicalDeviceToken(string(bytes.Repeat([]byte("a"), maxDeviceTokenLength))); !ok {
		t.Fatalf("maximum-length device token was rejected")
	}
	if _, ok := canonicalDeviceToken(string(bytes.Repeat([]byte("a"), maxDeviceTokenLength+1))); ok {
		t.Fatalf("oversized device token was accepted")
	}
	if _, ok := canonicalDeviceToken(string(bytes.Repeat([]byte("z"), minDeviceTokenLength))); ok {
		t.Fatalf("non-hex device token was accepted")
	}
	canonical, ok := canonicalDeviceToken(" ABCDEF0123456789ABCDEF0123456789 ")
	if !ok || canonical != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("device token was not canonicalized")
	}
}

func TestUpsertCurrentPushDeviceLimitsActiveDevicesPerUser(t *testing.T) {
	user := setupPushDeviceTest(t)
	for index := 0; index < maxActivePushDevicesPerUser+1; index++ {
		response := runPushDeviceRequest(user.ID, UpsertCurrentPushDevice, http.MethodPut, gin.H{
			"device_token":    fmt.Sprintf("%064x", index+1),
			"installation_id": fmt.Sprintf("00000000-0000-4000-8000-%012x", index+1),
			"platform":        "ios",
			"environment":     "development",
		})
		if response.Code != http.StatusNoContent {
			t.Fatalf("registration %d status = %d body = %s", index, response.Code, response.Body.String())
		}
	}

	var activeDeviceCount int64
	if err := database.DB.Model(&models.PushDevice{}).Where("user_id = ? AND enabled = ?", user.ID, true).Count(&activeDeviceCount).Error; err != nil {
		t.Fatalf("count active push devices: %v", err)
	}
	if activeDeviceCount != maxActivePushDevicesPerUser {
		t.Fatalf("active device count = %d, want %d", activeDeviceCount, maxActivePushDevicesPerUser)
	}
}
