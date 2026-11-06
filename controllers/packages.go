package controllers

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/big"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
	"net/http"
	"strings"
)

// GetPackages returns list of all packages
// @Summary Get list of packages
// @Description Get all packages for the authenticated user
// @Tags Packages
// @Security BearerAuth
// @Produce  json
// @Success 200 {array} models.Package
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/packages [get]
func GetPackages(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)

	var packages []models.Package
	if err := database.DB.Where("workspace_id = ?", workspaceID).Find(&packages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch packages"})
		return
	}

	c.JSON(http.StatusOK, packages)
}

// AddPackage adds a new package
// @Summary Add a new package
// @Description Create a new package for the authenticated user
// @Tags Packages
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param package body models.PackageCreateDTO true "Package data"
// @Success 201 {object} models.Package
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/packages [post]
func AddPackage(c *gin.Context) {
	userID := c.MustGet("userID").(uint)
	workspaceID := c.MustGet("workspaceID").(uint)

	var requestData models.PackageCreateDTO
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if services.ValidateCostDecimal(requestData.NetWeightG.Value, 3, false) != nil || services.ValidateCostDecimal(requestData.UnitCost.Value, 2, true) != nil {
		c.JSON(400, gin.H{"error": "Package weight must be positive and unit cost non-negative, within supported precision"})
		return
	}

	// Create Package model from DTO
	newPackage := models.Package{
		Name:        requestData.Name,
		NetWeightG:  requestData.NetWeightG.Value,
		UnitCost:    requestData.UnitCost.Value,
		UserID:      userID,
		WorkspaceID: &workspaceID,
	}

	if err := database.DB.Create(&newPackage).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add package"})
		return
	}

	c.JSON(http.StatusCreated, newPackage)
}

type packagePatch struct {
	Name                        *string                `json:"name"`
	NetWeightG                  models.OptionalDecimal `json:"net_weight_g" swaggertype:"number"`
	UnitCost                    models.OptionalDecimal `json:"unit_cost" swaggertype:"number"`
	ConfirmWeightInitialization bool                   `json:"confirm_weight_initialization"`
}

// UpdatePackage edits packaging metadata, guarding established used-format weights.
// @Summary Update a package
// @Tags Packages
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "Package ID"
// @Param request body packagePatch true "Package values and initialization confirmation"
// @Success 200 {object} models.Package
// @Failure 409 {object} map[string]string
// @Router /api/packages/{id} [patch]
func UpdatePackage(c *gin.Context) {
	id, err := costingID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid package ID"})
		return
	}
	var input packagePatch
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "Invalid package values"})
		return
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) == "" {
		c.JSON(400, gin.H{"error": "Package name is required"})
		return
	}
	if services.ValidateCostDecimal(input.NetWeightG.Value, 3, false) != nil || services.ValidateCostDecimal(input.UnitCost.Value, 2, true) != nil {
		c.JSON(400, gin.H{"error": "Invalid package weight or cost precision"})
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)
	var pkg models.Package
	err = database.PackageMutationTransaction(database.DB, func(tx *gorm.DB) error {
		if err := database.LockProductGraph(tx, workspaceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&pkg).Error; err != nil {
			return err
		}
		if input.NetWeightG.Set {
			var used int64
			if err := tx.Model(&models.Product{}).Where("package_id = ? AND workspace_id = ?", id, workspaceID).Count(&used).Error; err != nil {
				return err
			}
			changed := true
			if pkg.NetWeightG == nil && input.NetWeightG.Value == nil {
				changed = false
			}
			if pkg.NetWeightG != nil && input.NetWeightG.Value != nil {
				a, _ := services.ParseCostDecimal(string(*pkg.NetWeightG))
				b, _ := services.ParseCostDecimal(string(*input.NetWeightG.Value))
				changed = a.Cmp(b) != 0
			}
			if changed && used > 0 && pkg.NetWeightG != nil {
				return costConflict("package_weight_in_use", "Create a new format to change an established used package weight")
			}
			if changed && used > 0 && pkg.NetWeightG == nil && input.NetWeightG.Value != nil {
				if !input.ConfirmWeightInitialization {
					return costConflict("package_initialization_requires_confirmation", "Review linked products and confirm first-time weight initialization")
				}
				var products []models.Product
				if err := tx.Where("package_id = ? AND workspace_id = ? AND (product_type = ? OR cost_mode = ?)", id, workspaceID, "assortment", models.CostModeAssortment).Preload("CostComponents", "workspace_id = ?", workspaceID).Preload("AssortmentComponents", "workspace_id = ?", workspaceID).Find(&products).Error; err != nil {
					return err
				}
				weight, _ := services.ParseCostDecimal(string(*input.NetWeightG.Value))
				for _, p := range products {
					sum := new(big.Rat)
					if p.CompositionBasis != nil && *p.CompositionBasis == "products" {
						for _, row := range p.AssortmentComponents {
							q, err := services.ParseCostDecimal(string(row.QuantityG))
							if err != nil {
								return costConflict("weight_mismatch", "Review the assortment composition before initializing package weight")
							}
							sum.Add(sum, q)
						}
					} else {
						for _, row := range p.CostComponents {
							q, err := services.ParseCostDecimal(string(row.QuantityG))
							if err != nil {
								return costConflict("weight_mismatch", "Review the assortment composition before initializing package weight")
							}
							sum.Add(sum, q)
						}
					}
					if sum.Cmp(weight) != 0 {
						return costConflict("weight_mismatch", "A linked assortment does not match this weight; correct its composition or package assignment")
					}
				}
			}
			pkg.NetWeightG = input.NetWeightG.Value
		}
		if input.Name != nil {
			pkg.Name = strings.TrimSpace(*input.Name)
		}
		if input.UnitCost.Set {
			pkg.UnitCost = input.UnitCost.Value
		}
		return tx.Omit("Products", "User", "Workspace").Save(&pkg).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "Package not found"})
		return
	}
	if err != nil {
		writeCostingError(c, err, "Failed to update package")
		return
	}
	c.JSON(200, pkg)
}
