package controllers

import (
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"mobile-backend-go/constants"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
)

type orderItemInput struct {
	ID        uint     `json:"id"`
	ProductID uint     `json:"product_id"`
	Quantity  int      `json:"quantity"`
	Price     float64  `json:"price"`
	CostPrice *float64 `json:"cost_price"`
}
type costedOrderInput struct {
	ClientID uint             `json:"client_id" binding:"required"`
	Date     time.Time        `json:"date"`
	Status   string           `json:"status"`
	Comment  string           `json:"comment"`
	Items    []orderItemInput `json:"items" binding:"required,min=1"`
}

func validOrderInput(input *costedOrderInput, create bool) error {
	if input.ClientID == 0 || len(input.Items) == 0 {
		return badCostInput("client_id and at least one item are required")
	}
	if create && input.Status == "" {
		input.Status = constants.OrderStatusNew
	}
	if !constants.IsValidOrderStatus(input.Status) {
		return badCostInput("Invalid order status")
	}
	for _, row := range input.Items {
		if row.ProductID == 0 || row.Quantity <= 0 || row.Price < 0 || math.IsNaN(row.Price) || math.IsInf(row.Price, 0) {
			return badCostInput("Order items require a product, positive quantity and non-negative finite price")
		}
		if row.CostPrice != nil && (*row.CostPrice < 0 || math.IsNaN(*row.CostPrice) || math.IsInf(*row.CostPrice, 0)) {
			return badCostInput("Order cost must be finite and non-negative")
		}
		if create && row.ID != 0 {
			return badCostInput("New orders cannot reference existing item IDs")
		}
	}
	return nil
}
func validateOrderClient(tx *gorm.DB, clientID, workspaceID uint) error {
	var count int64
	if err := tx.Model(&models.Client{}).Where("id = ? AND workspace_id = ?", clientID, workspaceID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return badCostInput("Invalid client ID or client does not belong to workspace")
	}
	return nil
}
func loadOrderProducts(tx *gorm.DB, workspaceID uint, ids []uint, calculate map[uint]bool) (map[uint]models.Product, error) {
	result := map[uint]models.Product{}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []models.Product
	if err := tx.Where("workspace_id = ? AND id IN ?", workspaceID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	estimateRows := []models.Product{}
	for _, row := range rows {
		result[row.ID] = row
		if calculate[row.ID] {
			estimateRows = append(estimateRows, row)
		}
	}
	for _, id := range ids {
		if _, exists := result[id]; !exists {
			return nil, badCostInput("Invalid product ID or product does not belong to workspace")
		}
	}
	if err := services.NewCosting(tx, workspaceID).AttachProducts(estimateRows); err != nil {
		return nil, err
	}
	for _, row := range estimateRows {
		result[row.ID] = row
	}
	return result, nil
}
func resolveNewOrderCost(product models.Product, assertion *float64, forceManual bool) (float64, *models.CostSnapshot, error) {
	if forceManual || product.CostMode == "" || product.CostMode == models.CostModeManual {
		amount := product.Cost
		if assertion != nil {
			amount = *assertion
		}
		if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
			return 0, nil, costConflict("cost_incomplete", "Manual product cost is invalid")
		}
		return amount, snapshotWithProvenance(product, models.CostModeManual, amount, nil), nil
	}
	estimate := product.CostEstimate
	if estimate == nil || estimate.Status != "complete" || estimate.TotalCost == nil {
		return 0, nil, costConflict("cost_incomplete", "Product cost is incomplete; complete the recipe and package configuration")
	}
	amount := *estimate.TotalCost
	if assertion != nil && !services.CostAssertionMatches(*assertion, amount) {
		return 0, nil, costConflict("cost_changed", "Product cost changed; refresh the estimate and review it before saving")
	}
	return amount, snapshotWithProvenance(product, product.CostMode, amount, estimate), nil
}

func addCostedOrder(c *gin.Context) {
	var input costedOrderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := validOrderInput(&input, true); err != nil {
		writeCostingError(c, err, "Invalid order")
		return
	}
	workspaceID, userID := c.MustGet("workspaceID").(uint), c.MustGet("userID").(uint)
	order := models.Order{ClientID: input.ClientID, Date: input.Date, Status: input.Status, Comment: input.Comment, UserID: userID, WorkspaceID: &workspaceID}
	if order.Date.IsZero() {
		order.Date = time.Now()
	}
	err := database.CostingTransaction(database.DB, false, func(tx *gorm.DB) error {
		if err := validateOrderClient(tx, input.ClientID, workspaceID); err != nil {
			return err
		}
		ids, calculate := []uint{}, map[uint]bool{}
		for _, row := range input.Items {
			ids = append(ids, row.ProductID)
			calculate[row.ProductID] = true
		}
		products, err := loadOrderProducts(tx, workspaceID, ids, calculate)
		if err != nil {
			return err
		}
		items := []models.OrderItem{}
		for _, row := range input.Items {
			amount, snapshot, err := resolveNewOrderCost(products[row.ProductID], row.CostPrice, false)
			if err != nil {
				return err
			}
			items = append(items, models.OrderItem{ProductID: row.ProductID, Quantity: row.Quantity, Price: row.Price, Cost_price: amount, CostSnapshot: snapshot})
		}
		if err := tx.Omit("Items", "Client", "User", "Workspace").Create(&order).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].OrderID = order.ID
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		return tx.Preload("Client").Preload("Items").Preload("Items.Product").Preload("Items.Product.Package").Where("id = ? AND workspace_id = ?", order.ID, workspaceID).First(&order).Error
	})
	if err != nil {
		writeCostingError(c, err, "Failed to create order")
		return
	}
	dispatchOrderNotification(services.OrderEventCreated, order.ID, workspaceID)
	c.JSON(http.StatusCreated, order)
}

type pendingOrderItem struct {
	input        orderItemInput
	saved        *models.OrderItem
	forceManual  bool
	needsCurrent bool
}

func updateCostedOrder(c *gin.Context) {
	id, err := costingID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid order ID"})
		return
	}
	var input costedOrderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := validOrderInput(&input, false); err != nil {
		writeCostingError(c, err, "Invalid order")
		return
	}
	workspaceID := c.MustGet("workspaceID").(uint)
	transition := false
	err = database.CostingTransaction(database.DB, false, func(tx *gorm.DB) error {
		var order models.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Items").Where("id = ? AND workspace_id = ?", id, workspaceID).First(&order).Error; err != nil {
			return err
		}
		if err := validateOrderClient(tx, input.ClientID, workspaceID); err != nil {
			return err
		}
		byID, byProduct := map[uint]*models.OrderItem{}, map[uint][]*models.OrderItem{}
		for i := range order.Items {
			row := &order.Items[i]
			byID[row.ID] = row
			byProduct[row.ProductID] = append(byProduct[row.ProductID], row)
		}
		used := map[uint]bool{}
		// Reserve explicit IDs first so ID-less legacy matching cannot consume them.
		for _, row := range input.Items {
			if row.ID != 0 {
				if byID[row.ID] == nil || used[row.ID] {
					return badCostInput("Item IDs must be unique and belong to this order")
				}
				used[row.ID] = true
			}
		}
		pending := []pendingOrderItem{}
		ids, calculate := []uint{}, map[uint]bool{}
		for _, row := range input.Items {
			candidate := pendingOrderItem{input: row}
			if row.ID != 0 {
				candidate.saved = byID[row.ID]
			} else {
				available := []*models.OrderItem{}
				for _, saved := range byProduct[row.ProductID] {
					if !used[saved.ID] {
						available = append(available, saved)
					}
				}
				if len(available) == 1 {
					candidate.saved = available[0]
					used[available[0].ID] = true
				}
				if len(available) > 1 {
					for _, saved := range available {
						calculated, err := services.IsCalculatedSnapshot(saved.CostSnapshot)
						if err != nil || calculated {
							return costConflict("ambiguous_order_items", "Refresh this order with a current client to preserve item identities")
						}
					}
					if row.CostPrice == nil {
						return costConflict("ambiguous_order_items", "Ambiguous manual replacements require explicit costs or item IDs")
					}
					candidate.forceManual = true
				}
			}
			if candidate.saved != nil {
				if _, err := services.IsCalculatedSnapshot(candidate.saved.CostSnapshot); err != nil {
					return costConflict("unsupported_cost_snapshot", err.Error())
				}
			}
			candidate.needsCurrent = candidate.saved == nil || candidate.saved.ProductID != row.ProductID
			if candidate.needsCurrent {
				ids = append(ids, row.ProductID)
				calculate[row.ProductID] = true
			}
			pending = append(pending, candidate)
		}
		products, err := loadOrderProducts(tx, workspaceID, ids, calculate)
		if err != nil {
			return err
		}
		items := []models.OrderItem{}
		for _, candidate := range pending {
			row := candidate.input
			item := models.OrderItem{OrderID: order.ID, ProductID: row.ProductID, Quantity: row.Quantity, Price: row.Price}
			if candidate.saved != nil {
				item = *candidate.saved
				item.ProductID, item.Quantity, item.Price = row.ProductID, row.Quantity, row.Price
			}
			if candidate.needsCurrent {
				amount, snapshot, err := resolveNewOrderCost(products[row.ProductID], row.CostPrice, candidate.forceManual)
				if err != nil {
					return err
				}
				item.Cost_price, item.CostSnapshot = amount, snapshot
			} else {
				calculated, _ := services.IsCalculatedSnapshot(item.CostSnapshot)
				if calculated {
					if row.CostPrice != nil && !services.CostAssertionMatches(*row.CostPrice, item.Cost_price) {
						return costConflict("historical_cost_read_only", "Historical calculated cost is read-only")
					}
				} else if row.CostPrice != nil && *row.CostPrice != item.Cost_price {
					item.Cost_price = *row.CostPrice
					if item.CostSnapshot == nil {
						item.CostSnapshot = services.NewCostSnapshot(models.CostModeManual, *row.CostPrice, nil)
					} else {
						copy := *item.CostSnapshot
						copy.Amount = *row.CostPrice
						item.CostSnapshot = &copy
					}
				}
			}
			items = append(items, item)
		}
		transition = isStatusTransition(order.Status, input.Status)
		order.ClientID, order.Status, order.Comment = input.ClientID, input.Status, input.Comment
		if !input.Date.IsZero() {
			order.Date = input.Date
		} else if order.Date.IsZero() {
			order.Date = order.CreatedAt
		}
		if err := tx.Omit("Items", "Client", "User", "Workspace").Save(&order).Error; err != nil {
			return err
		}
		kept := []uint{}
		for i := range items {
			if err := tx.Omit("Product", "Order").Save(&items[i]).Error; err != nil {
				return err
			}
			kept = append(kept, items[i].ID)
		}
		return tx.Where("order_id = ? AND id NOT IN ?", order.ID, kept).Delete(&models.OrderItem{}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "Order not found"})
		return
	}
	if err != nil {
		writeCostingError(c, err, "Failed to update order")
		return
	}
	if transition {
		dispatchOrderNotification(orderNotificationType(input.Status), id, workspaceID)
	}
	c.JSON(http.StatusOK, gin.H{"message": "Order updated successfully"})
}

func snapshotWithProvenance(product models.Product, source string, amount float64, estimate *models.CostEstimate) *models.CostSnapshot {
	snapshot := services.NewCostSnapshot(source, amount, estimate)
	details := []models.CompositionDetail{}
	if product.CostEstimate != nil && product.CostEstimate.CompositionDetails != nil {
		details = product.CostEstimate.CompositionDetails
	}
	snapshot.Provenance = &models.CostProvenance{ProductType: product.ProductType, ProductName: product.Name, CompositionBasis: product.CompositionBasis, Components: details, Package: models.CostProvenancePackage{ID: product.Package.ID, Name: product.Package.Name, NetWeightG: product.Package.NetWeightG, UnitCost: product.Package.UnitCost}}
	return snapshot
}
