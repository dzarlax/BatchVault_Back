package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

func setupAuthMiddlewareTest(t *testing.T, tokenVersion uint) models.User {
	t.Helper()

	gin.SetMode(gin.TestMode)
	t.Setenv("JWT_SECRET", "test-secret-value")

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

	user := models.User{Username: "token-user", Password: "hashed", TokenVersion: tokenVersion}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func issueTestToken(t *testing.T, userID uint, tokenVersion uint) string {
	t.Helper()

	claims := &Claims{
		UserID:       userID,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   "user:" + strconv.FormatUint(uint64(userID), 10),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func runAuthenticatedRequest(token string) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/protected", JWTMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"userID": c.MustGet("userID")})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestJWTMiddlewareAcceptsCurrentTokenVersion(t *testing.T) {
	user := setupAuthMiddlewareTest(t, 2)
	token := issueTestToken(t, user.ID, 2)

	response := runAuthenticatedRequest(token)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want %d", response.Code, response.Body.String(), http.StatusOK)
	}
}

func TestJWTMiddlewareRejectsStaleTokenVersion(t *testing.T) {
	user := setupAuthMiddlewareTest(t, 2)
	token := issueTestToken(t, user.ID, 1)

	response := runAuthenticatedRequest(token)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s, want %d", response.Code, response.Body.String(), http.StatusUnauthorized)
	}
}
