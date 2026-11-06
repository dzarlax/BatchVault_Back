package controllers

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
	"net/http"
	"strconv"
)

func validateRecipeWorkspace(db *gorm.DB, recipeIDs []uint, workspaceID uint) error {
	unique := map[uint]bool{}
	for _, id := range recipeIDs {
		if id == 0 {
			return badCostInput("recipe_ids cannot contain 0")
		}
		unique[id] = true
	}
	if len(unique) == 0 {
		return nil
	}
	ids := []uint{}
	for id := range unique {
		ids = append(ids, id)
	}
	var count int64
	if err := db.Model(&models.Recipe{}).Where("workspace_id = ? AND id IN ?", workspaceID, ids).Count(&count).Error; err != nil {
		return err
	}
	if int(count) != len(ids) {
		return badCostInput("Recipe does not belong to this workspace")
	}
	return nil
}

// GetProducts returns the workspace catalog with authoritative cost estimates.
// @Summary Get list of products
// @Tags Products
// @Security BearerAuth
// @Produce json
// @Success 200 {array} models.Product
// @Router /api/products [get]
func GetProducts(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	products := []models.Product{}
	err := database.CostingTransaction(database.DB, true, func(tx *gorm.DB) error {
		if err := tx.Preload("Options").Preload("Package", "workspace_id = ?", workspaceID).Where("workspace_id = ?", workspaceID).Find(&products).Error; err != nil {
			return err
		}
		return services.NewCosting(tx, workspaceID).AttachProducts(products)
	})
	if err != nil {
		writeCostingError(c, err, "Failed to fetch products")
		return
	}
	c.JSON(http.StatusOK, products)
}

// GetProductByID returns a product and its current cost breakdown.
// @Summary Get product by ID
// @Tags Products
// @Security BearerAuth
// @Produce json
// @Param id path int true "Product ID"
// @Success 200 {object} models.Product
// @Router /api/products/{id} [get]
func GetProductByID(c *gin.Context) {
	id, err := costingID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid product ID"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)
	var product models.Product
	err = database.CostingTransaction(database.DB, true, func(tx *gorm.DB) error {
		if err := tx.Preload("Options").Preload("Package", "workspace_id = ?", workspaceID).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&product).Error; err != nil {
			return err
		}
		rows := []models.Product{product}
		if err := services.NewCosting(tx, workspaceID).AttachProducts(rows); err != nil {
			return err
		}
		product = rows[0]
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "Product not found"})
		return
	}
	if err != nil {
		writeCostingError(c, err, "Failed to fetch product")
		return
	}
	c.JSON(200, product)
}

type ProductWriteRequest struct {
	Name         string          `json:"name" binding:"required,min=1"`
	Description  string          `json:"description"`
	Price        float64         `json:"price" binding:"required,min=0"`
	Cost         *float64        `json:"cost" binding:"omitempty,min=0"`
	Image        *string         `json:"image"`
	RecipeIDs    *[]uint         `json:"recipe_ids"`
	PackageID    uint            `json:"package_id" binding:"required"`
	CostMode     *string         `json:"cost_mode"`
	CostRecipeID json.RawMessage `json:"cost_recipe_id" swaggertype:"integer"`
	Components   json.RawMessage `json:"components" swaggertype:"object"`
}

// productWriteDocumentation describes the JSON shape; the handler additionally tracks omitted/null fields.
type productWriteDocumentation struct {
	Name         string                             `json:"name" binding:"required"`
	Description  string                             `json:"description"`
	Price        float64                            `json:"price" binding:"required"`
	Cost         *float64                           `json:"cost"`
	Image        *string                            `json:"image"`
	RecipeIDs    []uint                             `json:"recipe_ids"`
	PackageID    uint                               `json:"package_id" binding:"required"`
	CostMode     *string                            `json:"cost_mode" enums:"manual,recipe,assortment"`
	CostRecipeID *uint                              `json:"cost_recipe_id"`
	Components   []models.ProductCostComponentInput `json:"components"`
}

// CreateProduct creates a manual, recipe-based or fixed-assortment product.
// @Summary Create a product
// @Tags Products
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body productWriteDocumentation true "Product configuration"
// @Success 201 {object} models.Product
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/products [post]
func CreateProduct(c *gin.Context) { saveCostedProduct(c, false) }

// UpdateProduct updates product configuration without implicitly changing its cost source.
// @Summary Update a product
// @Tags Products
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "Product ID"
// @Param request body productWriteDocumentation true "Product configuration"
// @Success 200 {object} models.Product
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /api/products/{id} [put]
func UpdateProduct(c *gin.Context) { saveCostedProduct(c, true) }

func saveCostedProduct(c *gin.Context, update bool) {
	var input ProductWriteRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if math.IsNaN(input.Price) || math.IsInf(input.Price, 0) || input.Price < 0 || (input.Cost != nil && (math.IsNaN(*input.Cost) || math.IsInf(*input.Cost, 0) || *input.Cost < 0)) {
		c.JSON(400, gin.H{"error": "Prices must be finite and non-negative"})
		return
	}
	userID := c.MustGet("userID").(uint)
	workspaceID := c.MustGet("workspaceID").(uint)
	var id uint
	if update {
		var err error
		id, err = costingID(c)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid product ID"})
			return
		}
	}
	var product models.Product
	err := database.CostingTransaction(database.DB, false, func(tx *gorm.DB) error {
		var pkg models.Package
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND workspace_id = ?", input.PackageID, workspaceID).First(&pkg).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return badCostInput("Package does not belong to this workspace")
			}
			return err
		}
		if update {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("CostComponents", "workspace_id = ?", workspaceID).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&product).Error; err != nil {
				return err
			}
		} else {
			product.UserID = userID
			product.WorkspaceID = &workspaceID
			product.CostMode = models.CostModeManual
		}
		if product.CostMode == "" {
			product.CostMode = models.CostModeManual
		}
		if input.CostMode != nil && *input.CostMode != product.CostMode {
			product.CostMode = *input.CostMode
			product.CostRecipeID = nil
			product.CostComponents = nil
		}
		if input.CostRecipeID != nil {
			if err := json.Unmarshal(input.CostRecipeID, &product.CostRecipeID); err != nil {
				return badCostInput("Invalid cost_recipe_id")
			}
		}
		replaceComponents := !update || input.CostMode != nil || input.Components != nil
		if input.Components != nil {
			var rows []models.ProductCostComponentInput
			if err := json.Unmarshal(input.Components, &rows); err != nil {
				return badCostInput("Invalid cost components")
			}
			product.CostComponents = nil
			for i, row := range rows {
				product.CostComponents = append(product.CostComponents, models.ProductCostComponent{WorkspaceID: workspaceID, RecipeID: row.RecipeID, QuantityG: row.QuantityG, Position: i})
			}
		}
		if product.CostMode != models.CostModeManual && input.Cost != nil {
			return costConflict("calculated_cost_read_only", "Calculated product cost is read-only; select Manual explicitly to enter a cost")
		}
		if input.Cost != nil {
			product.Cost = *input.Cost
		}
		product.Name, product.Description, product.Price, product.PackageID = input.Name, input.Description, input.Price, input.PackageID
		if input.Image != nil {
			product.Image = *input.Image
		}
		costing := services.NewCosting(tx, workspaceID)
		if err := costing.ValidateProductConfig(product); err != nil {
			return err
		}
		if input.RecipeIDs != nil {
			if err := validateRecipeWorkspace(tx, *input.RecipeIDs, workspaceID); err != nil {
				return err
			}
		}
		query := tx.Omit("CostComponents", "Options", "Package", "User", "Workspace")
		if update {
			if err := query.Save(&product).Error; err != nil {
				return err
			}
		} else {
			if err := query.Create(&product).Error; err != nil {
				return err
			}
		}
		if replaceComponents {
			if err := tx.Where("product_id = ? AND workspace_id = ?", product.ID, workspaceID).Delete(&models.ProductCostComponent{}).Error; err != nil {
				return err
			}
			for i := range product.CostComponents {
				product.CostComponents[i].ID = 0
				product.CostComponents[i].ProductID = product.ID
			}
			if len(product.CostComponents) > 0 {
				if err := tx.Create(&product.CostComponents).Error; err != nil {
					return err
				}
			}
		}
		if input.RecipeIDs != nil {
			if err := tx.Where("product_id = ?", product.ID).Delete(&models.ProductOption{}).Error; err != nil {
				return err
			}
			for _, recipeID := range *input.RecipeIDs {
				if err := tx.Create(&models.ProductOption{ProductID: product.ID, RecipeID: recipeID, UserID: userID}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Preload("Options").Preload("Package", "workspace_id = ?", workspaceID).Where("id = ? AND workspace_id = ?", product.ID, workspaceID).First(&product).Error; err != nil {
			return err
		}
		rows := []models.Product{product}
		if err := costing.AttachProducts(rows); err != nil {
			return err
		}
		product = rows[0]
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "Product not found"})
		return
	}
	if err != nil {
		writeCostingError(c, err, "Failed to save product")
		return
	}
	status := http.StatusCreated
	if update {
		status = http.StatusOK
	}
	c.JSON(status, product)
}

// DeleteProduct deletes an existing product by its ID
// @Summary Delete an existing product
// @Description Delete a product by providing the product ID
// @Tags Products
// @Security BearerAuth
// @Param id path int true "Product ID"
// @Success 200 {object} map[string]string "message": "Product deleted successfully"
// @Failure 400 {object} map[string]string "Invalid product ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 500 {object} map[string]string "Failed to delete product"
// @Router /api/products/{id} [delete]
func DeleteProduct(c *gin.Context) {
	// Get product ID from URL parameters
	productID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product ID"})
		return
	}

	workspaceID := c.MustGet("workspaceID").(uint)

	// Check product existence and user ownership
	var product models.Product
	if err := database.DB.Where("id = ? AND workspace_id = ?", productID, workspaceID).First(&product).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	// Start transaction for deleting product and related data
	tx := database.DB.Begin()

	if err := tx.Where("product_id = ? AND workspace_id = ?", product.ID, workspaceID).Delete(&models.ProductCostComponent{}).Error; err != nil {
		tx.Rollback()
		c.JSON(500, gin.H{"error": "Failed to delete product composition"})
		return
	}

	// Delete related product options
	if err := tx.Where("product_id = ?", product.ID).Delete(&models.ProductOption{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product options"})
		return
	}

	// Delete the product itself
	if err := tx.Delete(&product).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// Return successful response
	c.JSON(http.StatusOK, gin.H{"message": "Product deleted successfully"})
}
