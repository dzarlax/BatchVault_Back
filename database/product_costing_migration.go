package database

import "gorm.io/gorm"

// MigrateProductCosting preserves manual values and installs active-composition uniqueness.
func MigrateProductCosting(db *gorm.DB) error {
	if err := db.Exec("UPDATE products SET cost_mode = 'manual' WHERE cost_mode IS NULL OR cost_mode = ''").Error; err != nil {
		return err
	}
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_product_cost_components_active_recipe ON product_cost_components (product_id, recipe_id) WHERE deleted_at IS NULL").Error
}
