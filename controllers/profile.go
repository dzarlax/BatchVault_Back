package controllers

import (
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ChangePasswordRequest represents password change request structure
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// ChangePassword changes user password
// @Summary Change user password
// @Description Change the password for the authenticated user
// @Tags Profile
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param body body ChangePasswordRequest true "Password change data"
// @Success 200 {object} map[string]string "Password changed successfully"
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "User not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/profile/change-password [post]
func ChangePassword(c *gin.Context) {
	userID := c.MustGet("userID").(uint)

	var payload ChangePasswordRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	if len(payload.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters long"})
		return
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(payload.CurrentPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Current password is incorrect"})
		return
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(payload.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Update user password and revoke already issued JWTs.
	result := database.DB.Model(&user).Updates(map[string]interface{}{
		"password":      string(hashedPassword),
		"token_version": gorm.Expr("token_version + ?", 1),
	})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to change password"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}
