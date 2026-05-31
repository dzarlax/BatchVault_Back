package middleware

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"log"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/http"
	"os"
	"strings"

	"gorm.io/gorm"
)

type Claims struct {
	UserID       uint `json:"userID"`
	TokenVersion uint `json:"tokenVersion"`
	jwt.RegisteredClaims
}

// ValidateJWTSecret checks that JWT_SECRET is set and has sufficient length.
func ValidateJWTSecret() error {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return fmt.Errorf("JWT_SECRET environment variable is not set")
	}
	if len(secret) < 16 {
		return fmt.Errorf("JWT_SECRET must be at least 16 characters long for security")
	}
	return nil
}

// JWTMiddleware validates JWT token in requests and sets userID in context
func JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header missing"})
			c.Abort()
			return
		}

		// Remove "Bearer " prefix from token
		if len(tokenString) > 7 && strings.ToUpper(tokenString[:7]) == "BEARER " {
			tokenString = tokenString[7:]
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			// Check signing method to protect against "none algorithm" attack
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(os.Getenv("JWT_SECRET")), nil
		})

		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Token has expired"})
				c.Abort()
				return
			}
			log.Printf("Invalid token: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		if !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		var user models.User
		if err := database.DB.Select("id", "token_version").First(&user, claims.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
				c.Abort()
				return
			}
			log.Printf("Failed to validate token version for user %d: %v", claims.UserID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate token"})
			c.Abort()
			return
		}
		if claims.TokenVersion != user.TokenVersion {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// Set userID in context for use in controllers
		c.Set("userID", claims.UserID)
		c.Next()
	}
}
