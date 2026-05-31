package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

func setupProfileTest(t *testing.T, tokenVersion uint) models.User {
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

	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	database.DB = db

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := models.User{Username: "profile-user", Password: string(passwordHash), TokenVersion: tokenVersion}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func runChangePasswordRequest(userID uint, body any) *httptest.ResponseRecorder {
	router := gin.New()
	router.POST("/profile/change-password", func(c *gin.Context) {
		c.Set("userID", userID)
		ChangePassword(c)
	})

	payload, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/profile/change-password", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestChangePasswordIncrementsTokenVersion(t *testing.T) {
	user := setupProfileTest(t, 4)

	response := runChangePasswordRequest(user.ID, gin.H{
		"currentPassword": "old-password",
		"newPassword":     "new-password",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want %d", response.Code, response.Body.String(), http.StatusOK)
	}

	var updated models.User
	if err := database.DB.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updated.TokenVersion != 5 {
		t.Fatalf("token version = %d, want 5", updated.TokenVersion)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte("new-password")); err != nil {
		t.Fatalf("new password hash does not match: %v", err)
	}
}
