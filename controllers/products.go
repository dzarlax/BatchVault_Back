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
	ProductType          *string         `json:"product_type"`
	CompositionBasis     json.RawMessage `json:"composition_basis"`
	ConfigurationVersion *uint           `json:"configuration_version"`
	RecipeID             json.RawMessage `json:"recipe_id"`
	AssortmentComponents json.RawMessage `json:"assortment_components"`
	Name                 string          `json:"name" binding:"required,min=1"`
	Description          string          `json:"description"`
	Price                float64         `json:"price" binding:"required,min=0"`
	Cost                 *float64        `json:"cost" binding:"omitempty,min=0"`
	Image                *string         `json:"image"`
	RecipeIDs            *[]uint         `json:"recipe_ids"`
	PackageID            uint            `json:"package_id" binding:"required"`
	CostMode             *string         `json:"cost_mode"`
	CostRecipeID         json.RawMessage `json:"cost_recipe_id" swaggertype:"integer"`
	Components           json.RawMessage `json:"components" swaggertype:"object"`
}

// productWriteDocumentation describes the JSON shape; the handler additionally tracks omitted/null fields.
type productWriteDocumentation struct {
	ProductType          *string                             `json:"product_type" enums:"standard,assortment"`
	CompositionBasis     *string                             `json:"composition_basis" enums:"products,legacy_recipes"`
	ConfigurationVersion *uint                               `json:"configuration_version"`
	RecipeID             *uint                               `json:"recipe_id"`
	AssortmentComponents []models.ProductAssortmentComponent `json:"assortment_components"`
	Name                 string                              `json:"name" binding:"required"`
	Description          string                              `json:"description"`
	Price                float64                             `json:"price" binding:"required"`
	Cost                 *float64                            `json:"cost"`
	Image                *string                             `json:"image"`
	RecipeIDs            []uint                              `json:"recipe_ids"`
	PackageID            uint                                `json:"package_id" binding:"required"`
	CostMode             *string                             `json:"cost_mode" enums:"manual,recipe,assortment"`
	CostRecipeID         *uint                               `json:"cost_recipe_id"`
	Components           []models.ProductCostComponentInput  `json:"components"`
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
	if input.ProductType == nil && (input.RecipeID != nil || input.CompositionBasis != nil || input.AssortmentComponents != nil || input.ConfigurationVersion != nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "product_type is required for product type configuration"})
		return
	}
	var compositionBasis *string
	if input.CompositionBasis != nil {
		if err := json.Unmarshal(input.CompositionBasis, &compositionBasis); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid composition_basis"})
			return
		}
	}
	if input.ProductType != nil && !productTypesEnabled() {
		c.JSON(http.StatusConflict, gin.H{"error": "Product type configuration is disabled", "code": "feature_disabled"})
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
	err := database.PackageMutationTransaction(database.DB, func(tx *gorm.DB) error {
		if err := database.LockProductGraph(tx, workspaceID); err != nil {
			return err
		}
		var pkg models.Package
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND workspace_id = ?", input.PackageID, workspaceID).First(&pkg).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return badCostInput("Package does not belong to this workspace")
			}
			return err
		}
		if update {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("CostComponents", func(q *gorm.DB) *gorm.DB { return q.Where("workspace_id = ?", workspaceID).Order("position, id") }).Preload("AssortmentComponents", "workspace_id = ?", workspaceID).Preload("Options").Where("id = ? AND workspace_id = ?", id, workspaceID).First(&product).Error; err != nil {
				return err
			}
		} else {
			product.UserID = userID
			product.WorkspaceID = &workspaceID
			product.CostMode = models.CostModeManual
		}
		if product.ProductType == "" {
			product.ProductType = "standard"
		}
		if update && input.ProductType != nil {
			if input.ConfigurationVersion == nil {
				return badCostInput("configuration_version is required")
			}
			if *input.ConfigurationVersion != product.ConfigurationVersion {
				return costConflict("configuration_changed", "Product configuration changed; refresh before saving")
			}
		}
		if input.RecipeID != nil && input.CostRecipeID != nil {
			var a, b *uint
			if json.Unmarshal(input.RecipeID, &a) != nil || json.Unmarshal(input.CostRecipeID, &b) != nil {
				return badCostInput("Invalid recipe ID")
			}
			if (a == nil) != (b == nil) || (a != nil && *a != *b) {
				return badCostInput("recipe_id and cost_recipe_id conflict")
			}
		}
		if input.RecipeID != nil {
			input.CostRecipeID = input.RecipeID
		}
		if update && input.ProductType == nil {
			protected := (product.ProductType == "assortment" && product.CompositionBasis != nil && *product.CompositionBasis == "products") || (product.CostMode == "manual" && (product.CostRecipeID != nil || len(product.CostComponents) > 0))
			transition := input.CostMode != nil && *input.CostMode == "manual" && product.CostMode != "manual" && (product.CostRecipeID != nil || len(product.CostComponents) > 0)
			recipeChanged := false
			if input.CostRecipeID != nil {
				var requested *uint
				if err := json.Unmarshal(input.CostRecipeID, &requested); err != nil {
					return badCostInput("Invalid cost_recipe_id")
				}
				recipeChanged = (requested == nil) != (product.CostRecipeID == nil) || (requested != nil && product.CostRecipeID != nil && *requested != *product.CostRecipeID)
			}
			componentsChanged := input.Components != nil
			if input.Components != nil {
				var requested []models.ProductCostComponentInput
				if err := json.Unmarshal(input.Components, &requested); err != nil {
					return badCostInput("Invalid cost components")
				}
				if string(input.Components) != "null" && len(requested) == len(product.CostComponents) {
					componentsChanged = false
					for i, row := range requested {
						left, err1 := services.ParseCostDecimal(string(row.QuantityG))
						right, err2 := services.ParseCostDecimal(string(product.CostComponents[i].QuantityG))
						if row.RecipeID != product.CostComponents[i].RecipeID || err1 != nil || err2 != nil || left.Cmp(right) != 0 {
							componentsChanged = true
							break
						}
					}
				}
			}
			optionsChanged := false
			if input.RecipeIDs != nil {
				counts := map[uint]int{}
				for _, option := range product.Options {
					counts[option.RecipeID]++
				}
				for _, id := range *input.RecipeIDs {
					counts[id]--
				}
				for _, count := range counts {
					if count != 0 {
						optionsChanged = true
						break
					}
				}
			}
			if transition || (protected && (input.PackageID != product.PackageID || (input.CostMode != nil && *input.CostMode != product.CostMode) || (input.Cost != nil && *input.Cost != product.Cost) || recipeChanged || componentsChanged || input.AssortmentComponents != nil || optionsChanged)) {
				return costConflict("requires_current_client", "Update the client to change this product configuration")
			}
			if protected {
				input.RecipeIDs = nil
				if input.Cost != nil && *input.Cost == product.Cost {
					input.Cost = nil
				}
			}
		}
		if update && input.ProductType != nil && *input.ProductType == "assortment" && product.ProductType == "assortment" && product.CompositionBasis != nil && *product.CompositionBasis == "legacy_recipes" && (input.CompositionBasis == nil || (compositionBasis != nil && *compositionBasis == "legacy_recipes")) && input.Components != nil {
			var requested []models.ProductCostComponentInput
			if err := json.Unmarshal(input.Components, &requested); err != nil {
				return badCostInput("Invalid cost components")
			}
			equal := string(input.Components) != "null" && len(requested) == len(product.CostComponents)
			if equal {
				for i, row := range requested {
					left, err1 := services.ParseCostDecimal(string(row.QuantityG))
					right, err2 := services.ParseCostDecimal(string(product.CostComponents[i].QuantityG))
					if row.RecipeID != product.CostComponents[i].RecipeID || err1 != nil || err2 != nil || left.Cmp(right) != 0 {
						equal = false
						break
					}
				}
			}
			if !equal {
				return costConflict("legacy_composition_requires_conversion", "Convert the legacy assortment to product components before changing its composition")
			}
			input.Components = nil
		}
		if input.ProductType != nil {
			if *input.ProductType != "standard" && *input.ProductType != "assortment" {
				return badCostInput("Invalid product_type")
			}
			if product.ProductType != *input.ProductType {
				product.CostRecipeID = nil
				product.CostComponents = nil
				product.AssortmentComponents = nil
				product.CompositionBasis = nil
			}
			product.ProductType = *input.ProductType
			if product.ProductType == "standard" && compositionBasis != nil {
				return badCostInput("Standard products cannot have a composition basis")
			}
			if product.ProductType == "assortment" && input.CompositionBasis != nil {
				if compositionBasis == nil || (*compositionBasis != "products" && *compositionBasis != "legacy_recipes") {
					return badCostInput("Invalid composition_basis")
				}
				if product.CompositionBasis != nil && *product.CompositionBasis == "legacy_recipes" && *compositionBasis == "products" {
					if input.AssortmentComponents == nil {
						return badCostInput("Conversion requires explicit product components")
					}
					product.CostComponents = nil
				}
				if *compositionBasis == "legacy_recipes" && (!update || product.CompositionBasis == nil || *product.CompositionBasis != "legacy_recipes") {
					return badCostInput("New assortments require product components")
				}
				product.CompositionBasis = compositionBasis
			}
			if product.ProductType == "assortment" && product.CompositionBasis == nil {
				basis := "products"
				product.CompositionBasis = &basis
			}
		}
		if input.AssortmentComponents != nil {
			var rows []models.ProductAssortmentComponent
			if err := json.Unmarshal(input.AssortmentComponents, &rows); err != nil {
				return badCostInput("Invalid assortment components")
			}
			product.AssortmentComponents = rows
			for i := range product.AssortmentComponents {
				product.AssortmentComponents[i].WorkspaceID = workspaceID
			}
		}
		if product.CostMode == "" {
			product.CostMode = models.CostModeManual
		}
		modeChanged := input.CostMode != nil && *input.CostMode != product.CostMode
		if input.CostMode != nil && *input.CostMode != product.CostMode {
			product.CostMode = *input.CostMode
			if input.ProductType == nil {
				product.CostRecipeID = nil
				product.CostComponents = nil
			}
		}
		if input.ProductType == nil && modeChanged && product.CostMode != "manual" {
			if product.CostMode == "assortment" {
				product.ProductType = "assortment"
				basis := "legacy_recipes"
				product.CompositionBasis = &basis
			} else {
				product.ProductType = "standard"
				product.CompositionBasis = nil
			}
		}
		if !update && input.ProductType == nil && product.CostMode == "assortment" {
			product.ProductType = "assortment"
			basis := "legacy_recipes"
			product.CompositionBasis = &basis
		}
		if update && product.ProductType == "assortment" {
			var count int64
			if err := tx.Model(&models.ProductAssortmentComponent{}).Where("workspace_id = ? AND component_product_id = ?", workspaceID, product.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return costConflict("product_in_use", "Product is used by an assortment")
			}
		}
		if input.CostRecipeID != nil {
			if err := json.Unmarshal(input.CostRecipeID, &product.CostRecipeID); err != nil {
				return badCostInput("Invalid cost_recipe_id")
			}
		}
		replaceComponents := !update || (input.ProductType == nil && modeChanged) || input.Components != nil || (input.ProductType != nil && product.ProductType == "standard") || (compositionBasis != nil && *compositionBasis == "products")
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
		if update {
			product.ConfigurationVersion++
		} else {
			product.ConfigurationVersion = 1
		}
		query := tx.Omit("CostComponents", "AssortmentComponents", "Options", "Package", "User", "Workspace")
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
		if input.AssortmentComponents != nil || (input.ProductType != nil && product.ProductType == "standard") {
			if err := tx.Where("product_id = ? AND workspace_id = ?", product.ID, workspaceID).Delete(&models.ProductAssortmentComponent{}).Error; err != nil {
				return err
			}
			for i := range product.AssortmentComponents {
				product.AssortmentComponents[i].ID = 0
				product.AssortmentComponents[i].ProductID = product.ID
			}
			if len(product.AssortmentComponents) > 0 {
				if err := tx.Create(&product.AssortmentComponents).Error; err != nil {
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
	committed := product
	committed.RecipeID = committed.CostRecipeID
	product, err = readSavedCostedProduct(committed.ID, workspaceID)
	if err != nil {
		status := http.StatusCreated
		if update {
			status = http.StatusOK
		}
		c.JSON(status, struct {
			models.Product
			RefreshRequired bool `json:"refresh_required"`
		}{committed, true})
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
	id, err := costingID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid product ID"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)
	err = database.PackageMutationTransaction(database.DB, func(tx *gorm.DB) error {
		if err := database.LockProductGraph(tx, workspaceID); err != nil {
			return err
		}
		var product models.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&product).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.ProductAssortmentComponent{}).Where("workspace_id = ? AND component_product_id = ?", workspaceID, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return costConflict("product_in_use", "Product is used by an assortment")
		}
		if err := tx.Where("product_id = ? AND workspace_id = ?", id, workspaceID).Delete(&models.ProductAssortmentComponent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("product_id = ? AND workspace_id = ?", id, workspaceID).Delete(&models.ProductCostComponent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("product_id = ?", id).Delete(&models.ProductOption{}).Error; err != nil {
			return err
		}
		return tx.Delete(&product).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "Product not found"})
		return
	}
	if err != nil {
		writeCostingError(c, err, "Failed to delete product")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Product deleted successfully"})
}

var readSavedCostedProduct = func(id, workspaceID uint) (models.Product, error) {
	var product models.Product
	err := database.CostingTransaction(database.DB, true, func(tx *gorm.DB) error {
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
	return product, err
}
