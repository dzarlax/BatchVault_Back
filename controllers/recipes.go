package controllers

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
	"net/http"
	"strconv"
)

// GetRecipes returns workspace recipes with ingredient subtotals and explicit costing status.
// @Summary Get recipes
// @Tags Recipes
// @Security BearerAuth
// @Produce json
// @Param recipe_id query int false "Recipe ID"
// @Param ingredient_id query int false "Ingredient ID"
// @Success 200 {array} models.Recipe
// @Router /api/recipes [get]
func GetRecipes(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	ids := map[string]uint{}
	for _, key := range []string{"recipe_id", "ingredient_id"} {
		if raw := c.Query(key); raw != "" {
			value, err := strconv.ParseUint(raw, 10, 32)
			if err != nil || value == 0 {
				c.JSON(400, gin.H{"error": "Invalid " + key})
				return
			}
			ids[key] = uint(value)
		}
	}
	recipes := []models.Recipe{}
	err := database.CostingTransaction(database.DB, true, func(tx *gorm.DB) error {
		query := tx.Where("recipes.workspace_id = ?", workspaceID)
		if id := ids["recipe_id"]; id != 0 {
			query = query.Where("recipes.id = ?", id)
		}
		if id := ids["ingredient_id"]; id != 0 {
			query = query.Where("recipes.id IN (?)", tx.Model(&models.RecipeIngredient{}).Select("recipe_id").Where("ingredient_id = ?", id))
		}
		if err := query.Find(&recipes).Error; err != nil {
			return err
		}
		return services.NewCosting(tx, workspaceID).AttachRecipes(recipes)
	})
	if err != nil {
		writeCostingError(c, err, "Failed to fetch recipes")
		return
	}
	c.JSON(200, recipes)
}

// GetRecipe returns one recipe and its current content-cost readiness.
// @Summary Get a recipe
// @Tags Recipes
// @Security BearerAuth
// @Produce json
// @Param id path int true "Recipe ID"
// @Success 200 {object} models.Recipe
// @Router /api/recipes/{id} [get]
func GetRecipe(c *gin.Context) {
	id, err := costingID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid recipe ID"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)
	rows := []models.Recipe{}
	err = database.CostingTransaction(database.DB, true, func(tx *gorm.DB) error {
		var recipe models.Recipe
		if err := tx.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&recipe).Error; err != nil {
			return err
		}
		rows = append(rows, recipe)
		return services.NewCosting(tx, workspaceID).AttachRecipes(rows)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "Recipe not found"})
		return
	}
	if err != nil {
		writeCostingError(c, err, "Failed to fetch recipe")
		return
	}
	c.JSON(200, rows[0])
}

// CreateRecipe creates a recipe, optionally with a known planned finished yield.
// @Summary Create a recipe
// @Tags Recipes
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param recipe body models.RecipeCreateDTO true "Recipe data"
// @Success 201 {object} models.Recipe
// @Router /api/recipes [post]
func CreateRecipe(c *gin.Context) {
	var input models.RecipeCreateDTO
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := services.ValidateCostDecimal(input.PlannedYieldG.Value, 3, false); err != nil {
		c.JSON(400, gin.H{"error": "Planned yield must be positive grams with up to three decimals"})
		return
	}
	userID := c.MustGet("userID").(uint)
	workspaceID := c.MustGet("workspaceID").(uint)
	recipe := models.Recipe{Name: input.Name, UserID: userID, WorkspaceID: &workspaceID, PlannedYieldG: input.PlannedYieldG.Value}
	if err := database.DB.Create(&recipe).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to create recipe"})
		return
	}
	c.JSON(http.StatusCreated, recipe)
}

type recipeYieldPatch struct {
	PlannedYieldG models.OptionalDecimal `json:"planned_yield_g" swaggertype:"number"`
}

// UpdateRecipeYield sets or clears a recipe's planned finished output.
// @Summary Update planned recipe yield
// @Tags Recipes
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "Recipe ID"
// @Param request body recipeYieldPatch true "Planned finished yield in grams; null clears it"
// @Success 200 {object} models.Recipe
// @Router /api/recipes/{id} [patch]
func UpdateRecipeYield(c *gin.Context) {
	id, err := costingID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid recipe ID"})
		return
	}
	var input recipeYieldPatch
	if err := c.ShouldBindJSON(&input); err != nil || !input.PlannedYieldG.Set {
		c.JSON(400, gin.H{"error": "Provide planned_yield_g or null"})
		return
	}
	if err := services.ValidateCostDecimal(input.PlannedYieldG.Value, 3, false); err != nil {
		c.JSON(400, gin.H{"error": "Planned yield must be positive grams with up to three decimals"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)
	result := database.DB.Model(&models.Recipe{}).Where("id = ? AND workspace_id = ?", id, workspaceID).Update("planned_yield_g", input.PlannedYieldG.Value)
	if result.Error != nil {
		c.JSON(500, gin.H{"error": "Failed to update recipe yield"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(404, gin.H{"error": "Recipe not found"})
		return
	}
	GetRecipe(c)
}

// DeleteRecipe deletes a recipe by ID
// @Summary Delete a recipe
// @Description Delete a recipe by its ID for the authenticated user
// @Tags Recipes
// @Security BearerAuth
// @Param id path int true "Recipe ID"
// @Success 200 {object} map[string]string "Recipe deleted successfully"
// @Failure 400 {object} map[string]string "Invalid recipe ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Recipe not found"
// @Router /api/recipes/{id} [delete]
func DeleteRecipe(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	recipeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipe ID"})
		return
	}

	var recipe models.Recipe
	if err := database.DB.Where("id = ? AND workspace_id = ?", recipeID, workspaceID).First(&recipe).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Recipe not found"})
		return
	}

	if err := database.DB.Delete(&recipe).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete recipe"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Recipe deleted successfully"})
}
