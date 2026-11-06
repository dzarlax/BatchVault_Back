package database

import "gorm.io/gorm"

// MigrateProductCosting preserves manual values and installs active-composition uniqueness.
func MigrateProductCosting(db *gorm.DB) error {
	if err := db.Exec("UPDATE products SET cost_mode = 'manual' WHERE cost_mode IS NULL OR cost_mode = ''").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_product_cost_components_active_recipe ON product_cost_components (product_id, recipe_id) WHERE deleted_at IS NULL").Error; err != nil {
		return err
	}
	if db.Migrator().HasTable("product_assortment_components") {
		return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_assortment_component_active ON product_assortment_components (product_id, component_product_id) WHERE deleted_at IS NULL").Error
	}
	return nil
}

// PrepareProductTypes backfills legacy rows before defaults or constraints are added.
func PrepareProductTypes(db *gorm.DB) error {
	if !db.Migrator().HasTable("products") {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasColumn("products", "product_type") {
			if err := tx.Exec("ALTER TABLE products ADD COLUMN product_type text").Error; err != nil {
				return err
			}
		}
		if !tx.Migrator().HasColumn("products", "composition_basis") {
			if err := tx.Exec("ALTER TABLE products ADD COLUMN composition_basis text").Error; err != nil {
				return err
			}
		}
		backfill := "UPDATE products SET product_type = 'standard' WHERE product_type IS NULL OR product_type = ''"
		if tx.Migrator().HasColumn("products", "cost_mode") {
			backfill = "UPDATE products SET product_type = CASE WHEN cost_mode = 'assortment' THEN 'assortment' ELSE 'standard' END WHERE product_type IS NULL OR product_type = ''"
		}
		if err := tx.Exec(backfill).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE products SET composition_basis = 'legacy_recipes' WHERE product_type = 'assortment' AND composition_basis IS NULL").Error
	})
}
