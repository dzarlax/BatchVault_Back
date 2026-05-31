package database

import (
	"log"

	"gorm.io/gorm"
)

func migratePriceQuantityToDoublePrecision(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}

	var dataType string
	if err := db.Raw(`
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
			AND table_name = 'prices'
			AND column_name = 'quantity'
	`).Scan(&dataType).Error; err != nil {
		return err
	}
	if dataType == "" || dataType == "double precision" {
		return nil
	}

	if err := db.Exec(`
		ALTER TABLE prices
		ALTER COLUMN quantity TYPE double precision
		USING quantity::double precision
	`).Error; err != nil {
		return err
	}

	log.Printf("Migrated prices.quantity from %s to double precision", dataType)
	return nil
}
