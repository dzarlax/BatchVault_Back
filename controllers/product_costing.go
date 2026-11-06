package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
)

type costingHTTPError struct {
	status        int
	code, message string
}

func (e *costingHTTPError) Error() string { return e.message }
func badCostInput(message string) error {
	return &costingHTTPError{http.StatusBadRequest, "invalid_cost_configuration", message}
}
func costConflict(code, message string) error {
	return &costingHTTPError{http.StatusConflict, code, message}
}
func writeCostingError(c *gin.Context, err error, fallback string) {
	var e *costingHTTPError
	if errors.As(err, &e) {
		c.JSON(e.status, gin.H{"error": e.message, "code": e.code})
		return
	}
	var invalid *services.CostConfigError
	if errors.As(err, &invalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": invalid.Error(), "code": "invalid_cost_configuration"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": fallback})
}
func costingID(c *gin.Context) (uint, error) {
	n, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid ID")
	}
	return uint(n), nil
}

// PreviewProductCost calculates a draft without persisting rows or triggering notifications.
// @Summary Preview a product cost
// @Tags Products
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body models.ProductCostPreviewInput true "Draft costing inputs"
// @Success 200 {object} models.CostEstimate
// @Failure 400 {object} map[string]string
// @Router /api/products/cost-preview [post]
func PreviewProductCost(c *gin.Context) {
	var input models.ProductCostPreviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid cost preview"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)
	if input.CostMode == "" {
		input.CostMode = models.CostModeManual
	}
	var recipe, costRecipe *uint
	if input.RecipeID != nil {
		if err := json.Unmarshal(input.RecipeID, &recipe); err != nil {
			writeCostingError(c, badCostInput("Invalid recipe_id"), "Invalid preview")
			return
		}
	}
	if input.CostRecipeID != nil {
		if err := json.Unmarshal(input.CostRecipeID, &costRecipe); err != nil {
			writeCostingError(c, badCostInput("Invalid cost_recipe_id"), "Invalid preview")
			return
		}
	}
	if input.RecipeID != nil && input.CostRecipeID != nil && ((recipe == nil) != (costRecipe == nil) || (recipe != nil && *recipe != *costRecipe)) {
		writeCostingError(c, badCostInput("recipe_id and cost_recipe_id conflict"), "Invalid preview")
		return
	}
	if input.RecipeID != nil {
		costRecipe = recipe
	}

	product := models.Product{CostMode: input.CostMode, CostRecipeID: costRecipe, Cost: input.Cost, PackageID: input.PackageID, AssortmentComponents: input.AssortmentComponents, CompositionBasis: input.CompositionBasis}
	if input.ProductType != nil {
		product.ProductType = *input.ProductType
		if product.ProductType == "assortment" && product.CompositionBasis == nil {
			basis := "products"
			product.CompositionBasis = &basis
		}
	}
	for i, row := range input.Components {
		product.CostComponents = append(product.CostComponents, models.ProductCostComponent{RecipeID: row.RecipeID, QuantityG: row.QuantityG, WorkspaceID: workspaceID, Position: i})
	}
	var estimate *models.CostEstimate
	err := database.CostingTransaction(database.DB, true, func(tx *gorm.DB) error {
		s := services.NewCosting(tx, workspaceID)
		if err := s.ValidateProductConfig(product); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return badCostInput("Package does not belong to this workspace")
			}
			return err
		}
		rows := []models.Product{product}
		if err := s.AttachProducts(rows); err != nil {
			return err
		}
		estimate = rows[0].CostEstimate
		return nil
	})
	if err != nil {
		writeCostingError(c, err, "Failed to calculate product cost")
		return
	}
	c.JSON(http.StatusOK, estimate)
}
