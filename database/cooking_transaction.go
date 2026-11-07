package database

import (
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"mobile-backend-go/models"
)

// CookingTransaction serializes workspace inventory writes before order or lot locks.
func CookingTransaction(db *gorm.DB, workspaceID uint, fn func(*gorm.DB, *models.CookingWorkspaceState) error) error {
	apply := func(tx *gorm.DB) error {
		state := models.CookingWorkspaceState{WorkspaceID: workspaceID, Version: 1}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
			return err
		}
		query := tx.Where("workspace_id = ?", workspaceID)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&state).Error; err != nil {
			return err
		}
		return fn(tx, &state)
	}
	if db.Dialector.Name() == "postgres" {
		return db.Transaction(apply, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	return db.Transaction(apply)
}

// RecipeCookingMutation serializes ingredient graph writers with cooking snapshots.
func RecipeCookingMutation(db *gorm.DB, workspaceID, recipeID uint, fn func(*gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var recipe models.Recipe
		q := tx.Where("workspace_id = ? AND id = ?", workspaceID, recipeID)
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := q.First(&recipe).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

// CookingCostingTransaction retains a consistent pricing graph for full order edits.
// PostgreSQL retries a fresh snapshot after a competing workspace writer commits.
func CookingCostingTransaction(db *gorm.DB, ws uint, fn func(*gorm.DB, *models.CookingWorkspaceState) error) error {
	if db.Dialector.Name() != "postgres" {
		return CookingTransaction(db, ws, fn)
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = db.Transaction(func(tx *gorm.DB) error {
			state := models.CookingWorkspaceState{WorkspaceID: ws, Version: 1}
			if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; e != nil {
				return e
			}
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("workspace_id = ?", ws).First(&state).Error; e != nil {
				return e
			}
			return fn(tx, &state)
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		var pg *pgconn.PgError
		if err == nil || !errors.As(err, &pg) || (pg.Code != "40001" && pg.Code != "40P01") {
			return err
		}
	}
	return err
}
