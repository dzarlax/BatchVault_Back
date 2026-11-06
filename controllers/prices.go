package controllers

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"log"
	"math"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/http"
	"time"
)

const latestPriceOrder = "date DESC, created_at DESC, id DESC"

// AddPrice adds a new price
// @Summary Add a new price
// @Description Add a new price for an ingredient
// @Tags Prices
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param X-Workspace-ID header int false "Workspace ID"
// @Param price body models.PriceCreateDTO true "Price data"
// @Success 201 {object} models.Price
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/prices [post]
func AddPrice(c *gin.Context) {
	var requestData models.PriceCreateDTO
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if math.IsNaN(requestData.Price) || math.IsInf(requestData.Price, 0) || requestData.Price < 0 || math.IsNaN(requestData.Quantity) || math.IsInf(requestData.Quantity, 0) || requestData.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Price must be non-negative and quantity must be positive"})
		return
	}

	// Get userID from context
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)

	// Create Price model from DTO
	newPrice := models.Price{
		IngredientID: requestData.IngredientID,
		Price:        requestData.Price,
		Quantity:     requestData.Quantity,
		Unit:         requestData.Unit,
		Date:         requestData.Date,
		UserID:       userID.(uint),
		WorkspaceID:  &workspaceID,
	}

	var ingredient models.Ingredient
	if err := database.DB.First(&ingredient, requestData.IngredientID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ingredient ID"})
		return
	}
	if err := prepareWorkspaceIngredientForWrite(workspaceID, requestData.IngredientID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ingredient ID"})
			return
		}
		if errors.Is(err, database.ErrWorkspaceIngredientNotActive) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Ingredient is not in workspace"})
			return
		}
		log.Printf("Failed to validate price ingredient workspace membership: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate ingredient workspace membership"})
		return
	}

	// Set current date if not provided
	if newPrice.Date.IsZero() {
		newPrice.Date = time.Now()
	}

	if err := database.DB.Create(&newPrice).Error; err != nil {
		log.Printf("Failed to add price: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add price"})
		return
	}

	// Preload Ingredient for response
	if err := database.DB.Preload("Ingredient").First(&newPrice, newPrice.ID).Error; err != nil {
		log.Printf("Failed to load price with ingredient: %v", err)
	}

	c.JSON(http.StatusCreated, newPrice)
}

// GetPrices returns list of workspace prices
// @Summary Get list of prices
// @Description Get all prices for the current workspace with optional filters
// @Tags Prices
// @Security BearerAuth
// @Produce  json
// @Param X-Workspace-ID header int false "Workspace ID"
// @Param ingredient_id query int false "Ingredient ID"
// @Param date query string false "Date in YYYY-MM-DD format"
// @Param sort_column query string false "Column to sort by"
// @Param sort_direction query string false "Sort direction (ASC or DESC)"
// @Success 200 {array} models.Price
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/prices [get]
func GetPrices(c *gin.Context) {
	var prices []models.Price
	query := database.DB

	// Get userID from context
	if _, exists := c.Get("userID"); !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)

	ingredientID := c.Query("ingredient_id")
	date := c.Query("date")
	sortColumn := c.Query("sort_column")
	sortDirection := c.Query("sort_direction")

	validSortColumns := map[string]bool{"price": true, "quantity": true, "date": true, "ingredient_name": true, "ingredient_type": true, "unit": true}
	validSortDirections := map[string]bool{"ASC": true, "DESC": true}

	// Apply filters
	query = query.Where("workspace_id = ?", workspaceID)
	if ingredientID != "" {
		query = query.Where("ingredient_id = ?", ingredientID)
	}
	if date != "" {
		query = query.Where("DATE(date) = ?", date)
	}

	// Apply sorting
	if validSortColumns[sortColumn] && validSortDirections[sortDirection] {
		query = query.Order(sortColumn + " " + sortDirection)
	} else {
		query = query.Order(latestPriceOrder)
	}

	if err := query.Preload("Ingredient").Find(&prices).Error; err != nil {
		log.Printf("Failed to fetch prices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch prices"})
		return
	}

	c.JSON(http.StatusOK, prices)
}
