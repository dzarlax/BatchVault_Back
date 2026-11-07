package database

import (
	"gorm.io/gorm"
	"mobile-backend-go/models"
)

// MigrateCooking adds the isolated cooking ledger without inferring legacy stock.
func MigrateCooking(db *gorm.DB) error {
	return db.AutoMigrate(&models.CookingWorkspaceState{}, &models.CookingRun{},
		&models.CookingRunFlavor{}, &models.CookingRunOrder{}, &models.CookingEvent{},
		&models.CookingStockLot{}, &models.CookingStockAllocation{})
}
