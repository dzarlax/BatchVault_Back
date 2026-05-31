package controllers

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"log"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/utils"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Definition of Claims structure for JWT tokens
type Claims struct {
	UserID       uint `json:"userID"` // Changed to UserID
	TokenVersion uint `json:"tokenVersion"`
	jwt.RegisteredClaims
}

// Definition of structures for registration and login requests
type RegisterPayload struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginPayload struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register creates a new user
// @Summary Register a new user
// @Description Create a new user with a username and password
// @Tags Auth
// @Accept  json
// @Produce  json
// @Param user body RegisterPayload true "User data"
// @Success 201 {object} models.User
// @Failure 400 {object} map[string]string
// @Router /api/auth/register [post]
func Register(c *gin.Context) {
	var payload RegisterPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashedPassword, err := utils.HashPassword(payload.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	var user models.User
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		user = models.User{Username: payload.Username, Password: hashedPassword}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}

		if _, err := database.EnsurePersonalWorkspaceForUser(tx, user.ID); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
			return
		}

		log.Printf("Failed to register user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register user"})
		return
	}

	c.JSON(http.StatusCreated, user)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Login authenticates a user and returns a JWT
// @Summary Login a user
// @Description Authenticate a user and return a JWT token
// @Tags Auth
// @Accept  json
// @Produce  json
// @Param user body LoginPayload true "User credentials"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/auth/login [post]
func Login(c *gin.Context) {
	var payload LoginPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := database.DB.Where("username = ?", payload.Username).First(&user).Error; err != nil {
		// Do not reveal information about user existence
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if !utils.CheckPassword(user.Password, payload.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Token is valid for 24 hours (instead of 72)
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID:       user.ID,
		TokenVersion: user.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   "user:" + strconv.FormatUint(uint64(user.ID), 10),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":      tokenString,
		"expires_at": expirationTime,
	})
}
