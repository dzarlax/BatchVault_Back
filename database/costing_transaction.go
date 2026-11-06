package database

import (
	"database/sql"
	"gorm.io/gorm"
)

// CostingTransaction keeps every graph read in a single consistent PostgreSQL snapshot.
func CostingTransaction(db *gorm.DB, readOnly bool, fn func(*gorm.DB) error) error {
	if db.Dialector.Name() == "postgres" {
		return db.Transaction(fn, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: readOnly})
	}
	return db.Transaction(fn)
}

// PackageMutationTransaction observes newly committed product links after acquiring
// the package lock. Product writers acquire that same lock before linking a product.
// Repeatable-read would retain a pre-wait snapshot and miss those links.
func PackageMutationTransaction(db *gorm.DB, fn func(*gorm.DB) error) error {
	if db.Dialector.Name() == "postgres" {
		return db.Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	return db.Transaction(fn)
}
