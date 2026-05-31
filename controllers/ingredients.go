package controllers

import (
	"errors"
	"mobile-backend-go/constants"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateIngredient creates a new ingredient
// @Summary Create a new ingredient
// @Description Create a new ingredient with type and name
// @Tags Ingredients
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param ingredient body models.Ingredient true "Ingredient data"
// @Success 201 {object} models.Ingredient
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 409 {object} map[string]string "Ingredient already exists"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/ingredients [post]
func CreateIngredient(c *gin.Context) {
	var newIngredient models.Ingredient
	workspaceID := c.MustGet("workspaceID").(uint)

	if err := c.ShouldBindJSON(&newIngredient); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Normalize name (remove extra spaces)
	newIngredient.Name = strings.TrimSpace(newIngredient.Name)
	newIngredient.Type = strings.TrimSpace(newIngredient.Type)

	if newIngredient.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name cannot be empty"})
		return
	}

	if newIngredient.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Type cannot be empty"})
		return
	}

	// Check name uniqueness
	var existingIngredient models.Ingredient
	if err := database.DB.Where("name = ?", newIngredient.Name).First(&existingIngredient).Error; err == nil {
		workspaceIngredient, ensureErr := database.EnsureWorkspaceIngredient(database.DB, workspaceID, existingIngredient.ID)
		if ensureErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to link ingredient to workspace"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{
			"error":                   "Ingredient with this name already exists",
			"field":                   "name",
			"value":                   newIngredient.Name,
			"existing_id":             existingIngredient.ID,
			"workspace_ingredient_id": workspaceIngredient.ID,
			"workspace_linked":        true,
		})
		return
	}

	tx := database.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create ingredient"})
		return
	}

	if err := tx.Create(&newIngredient).Error; err != nil {
		tx.Rollback()
		// Check if this is a database-level uniqueness error
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			if err := database.DB.Where("name = ?", newIngredient.Name).First(&existingIngredient).Error; err == nil {
				workspaceIngredient, ensureErr := database.EnsureWorkspaceIngredient(database.DB, workspaceID, existingIngredient.ID)
				if ensureErr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to link ingredient to workspace"})
					return
				}
				c.JSON(http.StatusConflict, gin.H{
					"error":                   "Ingredient with this name already exists",
					"field":                   "name",
					"value":                   newIngredient.Name,
					"existing_id":             existingIngredient.ID,
					"workspace_ingredient_id": workspaceIngredient.ID,
					"workspace_linked":        true,
				})
				return
			}
			c.JSON(http.StatusConflict, gin.H{
				"error": "Ingredient with this name already exists",
				"field": "name",
				"value": newIngredient.Name,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create ingredient"})
		return
	}

	workspaceIngredient, err := database.EnsureWorkspaceIngredient(tx, workspaceID, newIngredient.ID)
	if err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to link ingredient to workspace"})
		return
	}
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create ingredient"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":                      newIngredient.ID,
		"created_at":              newIngredient.CreatedAt,
		"updated_at":              newIngredient.UpdatedAt,
		"type":                    newIngredient.Type,
		"name":                    newIngredient.Name,
		"workspace_ingredient_id": workspaceIngredient.ID,
		"workspace_linked":        true,
	})
}

// CheckIngredientExists checks if ingredient exists by name
// @Summary Check if ingredient exists
// @Description Check if an ingredient with the given name already exists
// @Tags Ingredients
// @Security BearerAuth
// @Produce  json
// @Param name query string true "Ingredient name to check"
// @Success 200 {object} map[string]interface{} "Check result"
// @Failure 400 {object} map[string]string "Bad request"
// @Router /api/ingredients/check [get]
func CheckIngredientExists(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name parameter is required"})
		return
	}

	var count int64
	database.DB.Model(&models.Ingredient{}).Where("name = ?", name).Count(&count)

	response := gin.H{
		"exists": count > 0,
		"name":   name,
	}

	// If ingredient exists, add its ID
	if count > 0 {
		var ingredient models.Ingredient
		if err := database.DB.Where("name = ?", name).First(&ingredient).Error; err == nil {
			response["existing_id"] = ingredient.ID
			response["type"] = ingredient.Type
		}
	}

	c.JSON(http.StatusOK, response)
}

// GetIngredients returns list of all ingredients
// @Summary Get list of ingredients
// @Description Get all ingredients
// @Tags Ingredients
// @Security BearerAuth
// @Produce  json
// @Success 200 {array} models.Ingredient
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/ingredients [get]
func GetIngredients(c *gin.Context) {
	var ingredients []models.Ingredient

	if err := database.DB.Find(&ingredients).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch ingredients"})
		return
	}

	c.JSON(http.StatusOK, ingredients)
}

// SearchIngredients searches the global ingredient dictionary.
// @Summary Search global ingredients
// @Description Search global ingredients by name. PostgreSQL deployments also rank typo-tolerant matches using trigram similarity.
// @Tags Ingredients
// @Security BearerAuth
// @Produce  json
// @Param query query string false "Search query"
// @Success 200 {array} models.Ingredient
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/ingredients/search [get]
func SearchIngredients(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	db := database.DB.Order("name ASC").Limit(50)
	if query != "" {
		normalizedQuery := strings.ToLower(query)
		if database.SupportsTrigramSearch {
			db = database.DB.
				Where("LOWER(name) LIKE ? OR similarity(LOWER(name), ?) > ?", "%"+normalizedQuery+"%", normalizedQuery, 0.28).
				Order(clause.Expr{
					SQL:  "CASE WHEN LOWER(name) LIKE ? THEN 0 ELSE 1 END, similarity(LOWER(name), ?) DESC, name ASC",
					Vars: []interface{}{"%" + normalizedQuery + "%", normalizedQuery},
				}).
				Limit(50)
		} else {
			db = db.Where("LOWER(name) LIKE ?", "%"+normalizedQuery+"%")
		}
	}

	var ingredients []models.Ingredient
	if err := db.Find(&ingredients).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search ingredients"})
		return
	}

	c.JSON(http.StatusOK, ingredients)
}

// GetWorkspaceIngredients returns active ingredients for the current workspace.
// @Summary Get workspace ingredients
// @Description Get active ingredients in the current workspace working set
// @Tags Workspace Ingredients
// @Security BearerAuth
// @Produce  json
// @Param X-Workspace-ID header int false "Workspace ID"
// @Success 200 {array} models.WorkspaceIngredient
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/workspace-ingredients [get]
func GetWorkspaceIngredients(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)

	var workspaceIngredients []models.WorkspaceIngredient
	if err := database.DB.
		Joins("JOIN ingredients ON ingredients.id = workspace_ingredients.ingredient_id").
		Where("workspace_ingredients.workspace_id = ? AND workspace_ingredients.active = ?", workspaceID, true).
		Preload("Ingredient").
		Order("ingredients.name ASC").
		Find(&workspaceIngredients).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch workspace ingredients"})
		return
	}
	attachLatestWorkspacePrices(workspaceID, workspaceIngredients)
	attachWorkspaceIngredientUnitContracts(workspaceIngredients)

	c.JSON(http.StatusOK, workspaceIngredients)
}

// AddWorkspaceIngredient links an existing ingredient to the current workspace.
// @Summary Add workspace ingredient
// @Description Add an existing global ingredient to the current workspace working set
// @Tags Workspace Ingredients
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param X-Workspace-ID header int false "Workspace ID"
// @Param ingredient body models.WorkspaceIngredientCreateDTO true "Workspace ingredient data"
// @Success 201 {object} models.WorkspaceIngredient
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/workspace-ingredients [post]
func AddWorkspaceIngredient(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)

	var requestData models.WorkspaceIngredientCreateDTO
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	workspaceIngredient, err := database.EnsureWorkspaceIngredient(database.DB, workspaceID, requestData.IngredientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ingredient ID"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to link ingredient to workspace"})
		return
	}
	if err := database.DB.Preload("Ingredient").First(workspaceIngredient, workspaceIngredient.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch workspace ingredient"})
		return
	}
	attachWorkspaceIngredientUnitContract(workspaceIngredient)

	c.JSON(http.StatusCreated, workspaceIngredient)
}

// UpdateWorkspaceIngredient updates workspace ingredient metadata.
// @Summary Update workspace ingredient
// @Description Update workspace ingredient metadata in the current workspace
// @Tags Workspace Ingredients
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param X-Workspace-ID header int false "Workspace ID"
// @Param id path int true "Workspace ingredient ID"
// @Param ingredient body models.WorkspaceIngredientUpdateDTO true "Workspace ingredient update"
// @Success 200 {object} models.WorkspaceIngredient
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 404 {object} map[string]string "Workspace ingredient not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/workspace-ingredients/{id} [patch]
func UpdateWorkspaceIngredient(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	workspaceIngredientID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ingredient ID"})
		return
	}

	var requestData models.WorkspaceIngredientUpdateDTO
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var workspaceIngredient models.WorkspaceIngredient
	if err := database.DB.Where("id = ? AND workspace_id = ?", workspaceIngredientID, workspaceID).First(&workspaceIngredient).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workspace ingredient not found"})
		return
	}

	updates := map[string]interface{}{}
	if requestData.Active != nil {
		updates["active"] = *requestData.Active
	}
	if requestData.Alias != nil {
		updates["alias"] = strings.TrimSpace(*requestData.Alias)
	}
	if requestData.Category != nil {
		updates["category"] = strings.TrimSpace(*requestData.Category)
	}

	if len(updates) > 0 {
		if err := database.DB.Model(&workspaceIngredient).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update workspace ingredient"})
			return
		}
	}
	if err := database.DB.Preload("Ingredient").First(&workspaceIngredient, workspaceIngredient.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch workspace ingredient"})
		return
	}
	attachWorkspaceIngredientUnitContract(&workspaceIngredient)

	c.JSON(http.StatusOK, workspaceIngredient)
}

// DeleteWorkspaceIngredient deactivates a workspace ingredient.
// @Summary Delete workspace ingredient
// @Description Deactivate an ingredient in the current workspace working set
// @Tags Workspace Ingredients
// @Security BearerAuth
// @Param X-Workspace-ID header int false "Workspace ID"
// @Param id path int true "Workspace ingredient ID"
// @Success 200 {object} map[string]string "Workspace ingredient deactivated"
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 404 {object} map[string]string "Workspace ingredient not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/workspace-ingredients/{id} [delete]
func DeleteWorkspaceIngredient(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	workspaceIngredientID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ingredient ID"})
		return
	}

	result := database.DB.Model(&models.WorkspaceIngredient{}).
		Where("id = ? AND workspace_id = ?", workspaceIngredientID, workspaceID).
		Update("active", false)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to deactivate workspace ingredient"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workspace ingredient not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Workspace ingredient deactivated"})
}

func attachLatestWorkspacePrices(workspaceID uint, workspaceIngredients []models.WorkspaceIngredient) {
	for index := range workspaceIngredients {
		var latestPrice models.Price
		if err := database.DB.
			Where("workspace_id = ? AND ingredient_id = ?", workspaceID, workspaceIngredients[index].IngredientID).
			Order(latestPriceOrder).
			First(&latestPrice).Error; err == nil {
			workspaceIngredients[index].LatestPrice = &latestPrice
		}
	}
}

func attachWorkspaceIngredientUnitContracts(workspaceIngredients []models.WorkspaceIngredient) {
	for index := range workspaceIngredients {
		attachWorkspaceIngredientUnitContract(&workspaceIngredients[index])
	}
}

func attachWorkspaceIngredientUnitContract(workspaceIngredient *models.WorkspaceIngredient) {
	contract := constants.ResolveIngredientUnitContract(
		workspaceIngredient.Ingredient.Type,
		workspaceIngredient.Category,
	)
	workspaceIngredient.UnitProfile = contract.UnitProfile
	workspaceIngredient.DefaultUnit = contract.DefaultUnit
	workspaceIngredient.AllowedUnits = contract.AllowedUnits
}
