package services

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"math/big"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

// CookingOrderTransaction preserves the legacy path on schemas without the new ledger.
func CookingOrderTransaction(db *gorm.DB, ws uint, fn func(*gorm.DB) error) error {
	if !db.Migrator().HasTable(&models.CookingWorkspaceState{}) {
		return database.CostingTransaction(db, false, fn)
	}
	return database.CookingCostingTransaction(db, ws, func(tx *gorm.DB, _ *models.CookingWorkspaceState) error { return fn(tx) })
}

// SyncCookingOrder is called only inside a workspace-locked order transaction.
func SyncCookingOrder(tx *gorm.DB, ws, userID, orderID uint, status string, deleted bool) error {
	if !tx.Migrator().HasTable(&models.CookingStockAllocation{}) {
		return nil
	}
	var allocations []models.CookingStockAllocation
	if e := tx.Where("workspace_id = ? AND order_id = ? AND kind = ? AND state = ?", ws, orderID, "order", "held").Order("id ASC").Find(&allocations).Error; e != nil {
		return e
	}
	var links int64
	if e := tx.Model(&models.CookingRunOrder{}).Where("workspace_id = ? AND order_id = ?", ws, orderID).Count(&links).Error; e != nil {
		return e
	}
	if len(allocations) == 0 && links == 0 {
		return nil
	}
	items := []models.OrderItem{}
	if !deleted {
		if e := tx.Where("order_id = ?", orderID).Find(&items).Error; e != nil {
			return e
		}
	}
	byID := map[uint]models.OrderItem{}
	for _, i := range items {
		byID[i.ID] = i
	}
	consumed := map[uint]*big.Rat{}
	var past []models.CookingStockAllocation
	if e := tx.Where("workspace_id = ? AND order_id = ? AND kind = ? AND state = ?", ws, orderID, "order", "consumed").Find(&past).Error; e != nil {
		return e
	}
	for _, a := range past {
		var originalLot models.CookingStockLot
		if err := tx.Where("workspace_id = ? AND id = ?", ws, a.LotID).First(&originalLot).Error; err != nil {
			return err
		}
		current, exists := byID[a.OrderItemID]
		if !exists || current.ProductID != originalLot.ProductID {
			continue
		}
		if consumed[a.OrderItemID] == nil {
			consumed[a.OrderItemID] = new(big.Rat)
		}
		n, e := stockNumber(a.Quantity)
		if e != nil {
			return e
		}
		consumed[a.OrderItemID].Add(consumed[a.OrderItemID], n)
	}
	budget := map[uint]*big.Rat{}
	for _, i := range items {
		budget[i.ID] = big.NewRat(int64(i.Quantity), 1)
		if consumed[i.ID] != nil {
			budget[i.ID].Sub(budget[i.ID], consumed[i.ID])
			if budget[i.ID].Sign() < 0 {
				budget[i.ID].SetInt64(0)
			}
		}
	}
	service := CookingService{DB: tx, WorkspaceID: ws, UserID: userID}
	changed := false
	for _, a := range allocations {
		var lot models.CookingStockLot
		if e := tx.Where("workspace_id = ? AND id = ?", ws, a.LotID).First(&lot).Error; e != nil {
			return e
		}
		n, e := stockNumber(a.Quantity)
		if e != nil {
			return e
		}
		item, ok := byID[a.OrderItemID]
		keep := new(big.Rat)
		if ok && !deleted && status != "canceled" && item.ProductID == lot.ProductID {
			keep.Set(n)
			if keep.Cmp(budget[item.ID]) > 0 {
				keep.Set(budget[item.ID])
			}
			budget[item.ID].Sub(budget[item.ID], keep)
		}
		release := new(big.Rat).Sub(n, keep)
		if release.Sign() > 0 {
			if e = service.releaseAllocation(tx, &a, stockText(release)); e != nil {
				return e
			}
			changed = true
		}
		if keep.Sign() > 0 && status == "finished" {
			// Reload after partial release so reserved totals remain exact.
			if e = tx.Where("workspace_id = ? AND id = ?", ws, lot.ID).First(&lot).Error; e != nil {
				return e
			}
			lot.Quantity, e = subtractStock(lot.Quantity, stockText(keep))
			if e != nil {
				return e
			}
			lot.Reserved, e = subtractStock(lot.Reserved, stockText(keep))
			if e != nil {
				return e
			}
			if e = tx.Save(&lot).Error; e != nil {
				return e
			}
			a.Quantity = stockText(keep)
			a.State = "consumed"
			if e = tx.Save(&a).Error; e != nil {
				return e
			}
			changed = true
		}
	}
	// Any linked order change invalidates stock/demand previews, even before stock exists.
	if changed || links > 0 {
		state, e := stockState(tx, ws)
		if e != nil {
			return e
		}
		state.Version++
		if e = tx.Save(&state).Error; e != nil {
			return e
		}
	}
	if changed {
		payload, _ := json.Marshal(map[string]any{"order_id": orderID, "status": status, "deleted": deleted})
		event := models.CookingEvent{WorkspaceID: ws, UserID: userID, OperationID: uuid.NewString(), Kind: "order_stock", RequestHash: fmt.Sprint(orderID, status, deleted), Payload: payload, Result: json.RawMessage("{}")}
		if e := tx.Create(&event).Error; e != nil {
			return e
		}
	}
	return nil
}
