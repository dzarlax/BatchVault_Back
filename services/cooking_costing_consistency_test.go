package services

import (
	"gorm.io/gorm"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"testing"
)

// An order quote must not combine an earlier product with a later package graph.
func TestCookingPostgresOrderSnapshotStaysConsistentAcrossCatalogCommit(t *testing.T) {
	s, _ := cookingStoreFixture(t, true)
	if e := database.CookingTransaction(s.DB, s.WorkspaceID, func(*gorm.DB, *models.CookingWorkspaceState) error { return nil }); e != nil {
		t.Fatal(e)
	}
	err := CookingOrderTransaction(s.DB, s.WorkspaceID, func(tx *gorm.DB) error {
		var product models.Product
		if e := tx.First(&product, 101).Error; e != nil {
			return e
		}
		before := product.ConfigurationVersion
		done := make(chan error, 1)
		go func() {
			done <- s.DB.Transaction(func(writer *gorm.DB) error {
				if e := writer.Model(&models.Package{}).Where("id = ?", product.PackageID).Update("net_weight_g", "125").Error; e != nil {
					return e
				}
				return writer.Model(&models.Product{}).Where("id = ?", 101).Update("configuration_version", before+1).Error
			})
		}()
		if e := <-done; e != nil {
			return e
		}
		var pkg models.Package
		if e := tx.First(&pkg, product.PackageID).Error; e != nil {
			return e
		}
		if pkg.NetWeightG == nil || *pkg.NetWeightG != "100.000" && *pkg.NetWeightG != "100" {
			t.Fatalf("mixed graph: product version %d with package weight %v", before, pkg.NetWeightG)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var pkg models.Package
	if e := s.DB.Joins("JOIN products ON products.package_id = packages.id").Where("products.id = 101").First(&pkg).Error; e != nil {
		t.Fatal(e)
	}
	if pkg.NetWeightG == nil || *pkg.NetWeightG != "125.000" && *pkg.NetWeightG != "125" {
		t.Fatal("competing catalog commit did not persist", pkg.NetWeightG)
	}
}
