package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type legacyWorkspaceForCurrencyMigration struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"not null"`
	Slug string `gorm:"not null"`
}

type legacyMoneyRecordForCurrencyMigration struct {
	ID    uint    `gorm:"primaryKey"`
	Price float64 `gorm:"not null"`
	Cost  float64 `gorm:"not null"`
}

func (legacyMoneyRecordForCurrencyMigration) TableName() string {
	return "legacy_money_records"
}

func (legacyWorkspaceForCurrencyMigration) TableName() string {
	return "workspaces"
}

func TestWorkspaceCurrencyMigrationDefaultsAndPreservesExistingValues(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&legacyWorkspaceForCurrencyMigration{}, &legacyMoneyRecordForCurrencyMigration{}); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	legacyWorkspace := legacyWorkspaceForCurrencyMigration{Name: "Legacy", Slug: "legacy"}
	if err := db.Create(&legacyWorkspace).Error; err != nil {
		t.Fatalf("create legacy workspace: %v", err)
	}
	moneyRecord := legacyMoneyRecordForCurrencyMigration{Price: 123.45, Cost: 67.89}
	if err := db.Create(&moneyRecord).Error; err != nil {
		t.Fatalf("create legacy money record: %v", err)
	}

	if err := db.AutoMigrate(&Workspace{}, &legacyMoneyRecordForCurrencyMigration{}); err != nil {
		t.Fatalf("migrate currency schema: %v", err)
	}
	var migrated Workspace
	if err := db.First(&migrated, legacyWorkspace.ID).Error; err != nil {
		t.Fatalf("load migrated workspace: %v", err)
	}
	if migrated.Currency != "RSD" {
		t.Fatalf("legacy workspace currency = %q, want RSD", migrated.Currency)
	}
	if err := db.Model(&Workspace{}).Where("id = ?", migrated.ID).Update("currency", "EUR").Error; err != nil {
		t.Fatalf("set workspace currency: %v", err)
	}

	if err := db.AutoMigrate(&Workspace{}, &legacyMoneyRecordForCurrencyMigration{}); err != nil {
		t.Fatalf("repeat currency migration: %v", err)
	}
	if err := db.First(&migrated, legacyWorkspace.ID).Error; err != nil {
		t.Fatalf("reload workspace after repeated migration: %v", err)
	}
	if migrated.Currency != "EUR" {
		t.Fatalf("repeated migration changed currency to %q, want EUR", migrated.Currency)
	}
	var preservedMoneyRecord legacyMoneyRecordForCurrencyMigration
	if err := db.First(&preservedMoneyRecord, moneyRecord.ID).Error; err != nil {
		t.Fatalf("reload money record after repeated migration: %v", err)
	}
	if preservedMoneyRecord.Price != moneyRecord.Price || preservedMoneyRecord.Cost != moneyRecord.Cost {
		t.Fatalf("repeated migration changed money fields: record = %+v, want price %.2f cost %.2f", preservedMoneyRecord, moneyRecord.Price, moneyRecord.Cost)
	}

	newWorkspace := Workspace{Name: "New", Slug: "new"}
	if err := db.Create(&newWorkspace).Error; err != nil {
		t.Fatalf("create new workspace: %v", err)
	}
	if newWorkspace.Currency != "RSD" {
		t.Fatalf("new workspace currency = %q, want RSD", newWorkspace.Currency)
	}
}
