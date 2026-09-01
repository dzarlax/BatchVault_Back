package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mobile-backend-go/constants"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetOrder returns an order by ID
// @Summary Get an order by ID
// @Description Fetch an order by its ID
// @Tags Orders
// @Security BearerAuth
// @Produce  json
// @Param id path int true "Order ID"
// @Success 200 {object} models.Order
// @Failure 400 {object} map[string]string "Invalid order ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Order not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/orders/{id} [get]
func GetOrder(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	orderID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	var order models.Order
	if err := database.DB.
		Where("id = ? AND workspace_id = ?", orderID, workspaceID).
		Preload("Client").
		Preload("Items").
		Preload("Items.Product").
		Preload("Items.Product.Package").
		First(&order).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}

	c.JSON(http.StatusOK, order)
}

// GetOrders returns a list of all orders
// @Summary Get list of orders
// @Description Get all orders for the authenticated user, sorted by order date (newest first)
// @Tags Orders
// @Security BearerAuth
// @Produce  json
// @Success 200 {array} models.Order
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/orders [get]
func GetOrders(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)

	var orders []models.Order
	if err := database.DB.
		Where("workspace_id = ?", workspaceID).
		Preload("Client").
		Preload("Items").
		Preload("Items.Product").
		Preload("Items.Product.Package").
		Order("date DESC, created_at DESC").
		Find(&orders).Error; err != nil {
		log.Printf("Failed to fetch orders for workspaceID %v: %v", workspaceID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch orders"})
		return
	}

	c.JSON(http.StatusOK, orders)
}

// AddOrder adds a new order
// @Summary Add a new order
// @Description Create a new order for the authenticated user
// @Tags Orders
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param order body object true "Order data"
// @Success 201 {object} models.Order
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/orders [post]
func AddOrder(c *gin.Context) {
	userID := c.MustGet("userID").(uint)
	workspaceID := c.MustGet("workspaceID").(uint)

	var requestData struct {
		ClientID uint      `json:"client_id" binding:"required"`
		Date     time.Time `json:"date"`
		Status   string    `json:"status"`
		Comment  string    `json:"comment"`
		Items    []struct {
			ProductID uint     `json:"product_id" binding:"required"`
			Quantity  int      `json:"quantity" binding:"required,min=1"`
			Price     float64  `json:"price" binding:"required,min=0"`
			CostPrice *float64 `json:"cost_price" binding:"omitempty,min=0"`
		} `json:"items" binding:"required,min=1"`
	}

	// Read data from request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Additional business rules validation
	if requestData.ClientID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
		return
	}

	// Validate order items
	for i, item := range requestData.Items {
		if item.ProductID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].product_id is required", i)})
			return
		}
		if item.Quantity <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].quantity must be greater than 0", i)})
			return
		}
		if item.Price < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].price cannot be negative", i)})
			return
		}
		if item.CostPrice != nil && *item.CostPrice < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].cost_price cannot be negative", i)})
			return
		}
	}

	if requestData.Status == "" {
		requestData.Status = constants.OrderStatusNew // Set default status
	}
	if !constants.IsValidOrderStatus(requestData.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order status"})
		return
	}

	// Check client existence and ownership
	var client models.Client
	if err := database.DB.Where("id = ? AND workspace_id = ?", requestData.ClientID, workspaceID).First(&client).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID or client does not belong to workspace"})
		return
	}

	orderDate := requestData.Date
	if orderDate.IsZero() {
		orderDate = time.Now()
	}

	// Create order
	newOrder := models.Order{
		ClientID:    requestData.ClientID,
		Date:        orderDate,
		Status:      requestData.Status,
		Comment:     requestData.Comment,
		UserID:      userID,
		WorkspaceID: &workspaceID,
	}

	// Start transaction
	tx := database.DB.Begin()

	// Save order
	if err := tx.Create(&newOrder).Error; err != nil {
		tx.Rollback()
		log.Printf("Failed to add order: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add order"})
		return
	}

	// Create order items with product validation and auto-filling cost_price
	var orderItems []models.OrderItem
	for _, item := range requestData.Items {
		// Fetch product for validation and cost
		var product models.Product
		if err := tx.Where("id = ? AND workspace_id = ?", item.ProductID, workspaceID).First(&product).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product ID or product does not belong to workspace"})
			return
		}

		// Determine cost_price: use provided value or get from product
		costPrice := product.Cost // Default to product cost
		if item.CostPrice != nil {
			costPrice = *item.CostPrice // Use provided value if specified
		}

		orderItems = append(orderItems, models.OrderItem{
			OrderID:    newOrder.ID,
			ProductID:  item.ProductID,
			Quantity:   item.Quantity,
			Price:      item.Price,
			Cost_price: costPrice,
		})
	}

	// Save order items
	if len(orderItems) > 0 {
		if err := tx.Create(&orderItems).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create order items"})
			return
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}
	dispatchOrderNotification(services.OrderEventCreated, newOrder.ID, workspaceID)

	// Load full order information
	var createdOrder models.Order
	if err := database.DB.
		Preload("Client").
		Preload("Items").
		Preload("Items.Product").
		Preload("Items.Product.Package").
		First(&createdOrder, newOrder.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load created order"})
		return
	}

	c.JSON(http.StatusCreated, createdOrder)
}

// UpdateOrder updates an order
// @Summary Update an order
// @Description Update an order's details
// @Tags Orders
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param id path int true "Order ID"
// @Param order body models.Order true "Order data"
// @Success 200 {object} map[string]string "Order updated successfully"
// @Failure 400 {object} map[string]string "Invalid order ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Order not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/orders/{id} [put]
func UpdateOrder(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	orderID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	var existingOrder models.Order
	if err := database.DB.Where("id = ? AND workspace_id = ?", orderID, workspaceID).Preload("Items").First(&existingOrder).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}

	var requestData struct {
		ClientID uint      `json:"client_id" binding:"required"`
		Date     time.Time `json:"date"`
		Status   string    `json:"status"`
		Comment  string    `json:"comment"`
		Items    []struct {
			ProductID uint     `json:"product_id" binding:"required"`
			Quantity  int      `json:"quantity" binding:"required,min=1"`
			Price     float64  `json:"price" binding:"required,min=0"`
			CostPrice *float64 `json:"cost_price" binding:"omitempty,min=0"`
		} `json:"items" binding:"required,min=1"`
	}

	// Read data from request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Additional business rules validation
	if requestData.ClientID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
		return
	}
	if !constants.IsValidOrderStatus(requestData.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order status"})
		return
	}

	// Validate order items
	for i, item := range requestData.Items {
		if item.ProductID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].product_id is required", i)})
			return
		}
		if item.Quantity <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].quantity must be greater than 0", i)})
			return
		}
		if item.Price < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].price cannot be negative", i)})
			return
		}
		if item.CostPrice != nil && *item.CostPrice < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("items[%d].cost_price cannot be negative", i)})
			return
		}
	}

	var client models.Client
	if err := database.DB.Where("id = ? AND workspace_id = ?", requestData.ClientID, workspaceID).First(&client).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID or client does not belong to workspace"})
		return
	}

	// Start transaction
	tx := database.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update order"})
		return
	}

	// Lock the current row while deciding whether this request changes its status.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND workspace_id = ?", orderID, workspaceID).
		First(&existingOrder).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update order"})
		}
		return
	}
	statusTransition := isStatusTransition(existingOrder.Status, requestData.Status)

	// Update order fields
	existingOrder.ClientID = requestData.ClientID
	if !requestData.Date.IsZero() {
		existingOrder.Date = requestData.Date
	} else if existingOrder.Date.IsZero() {
		existingOrder.Date = existingOrder.CreatedAt
	}
	existingOrder.Status = requestData.Status
	existingOrder.Comment = requestData.Comment

	// Save updated order
	if err := tx.Save(&existingOrder).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update order"})
		return
	}

	// Delete old order items
	if err := tx.Where("order_id = ?", existingOrder.ID).Delete(&models.OrderItem{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete old order items"})
		return
	}

	// Add new order items with auto-filling cost_price
	var newOrderItems []models.OrderItem
	for _, item := range requestData.Items {
		// Fetch product to get cost if needed
		var product models.Product
		if err := tx.Where("id = ? AND workspace_id = ?", item.ProductID, workspaceID).First(&product).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product ID or product does not belong to workspace"})
			return
		}

		// Determine cost_price: use provided value or get from product
		costPrice := product.Cost // Default to product cost
		if item.CostPrice != nil {
			costPrice = *item.CostPrice // Use provided value if specified
		}

		newOrderItems = append(newOrderItems, models.OrderItem{
			OrderID:    existingOrder.ID,
			ProductID:  item.ProductID,
			Quantity:   item.Quantity,
			Price:      item.Price,
			Cost_price: costPrice,
		})
	}

	// Save new order items
	if len(newOrderItems) > 0 {
		if err := tx.Create(&newOrderItems).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create new order items"})
			return
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}
	if statusTransition {
		dispatchOrderNotification(orderNotificationType(requestData.Status), existingOrder.ID, workspaceID)
	}

	// Return success response
	c.JSON(http.StatusOK, gin.H{"message": "Order updated successfully"})
}

// UpdateOrderStatus updates order status
// @Summary Update order status
// @Description Update the status of an order
// @Tags Orders
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param id path int true "Order ID"
// @Param status body map[string]string true "Order status"
// @Success 200 {object} map[string]string "Order status updated successfully"
// @Failure 400 {object} map[string]string "Invalid order ID or missing status"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Order not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/orders/{id}/status [put]
func UpdateOrderStatus(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	orderID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	var requestBody struct {
		Status string `json:"status"`
	}

	if err := c.ShouldBindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	if requestBody.Status == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required field: status"})
		return
	}
	if !constants.IsValidOrderStatus(requestBody.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order status"})
		return
	}

	result := database.DB.Model(&models.Order{}).
		Where("id = ? AND workspace_id = ? AND status <> ?", orderID, workspaceID, requestBody.Status).
		Update("status", requestBody.Status)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update order status"})
		return
	}
	if result.RowsAffected == 0 {
		var order models.Order
		if err := database.DB.Select("id").Where("id = ? AND workspace_id = ?", orderID, workspaceID).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update order status"})
			}
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Order status updated successfully"})
		return
	}

	dispatchOrderNotification(orderNotificationType(requestBody.Status), uint(orderID), workspaceID)

	c.JSON(http.StatusOK, gin.H{"message": "Order status updated successfully"})
}

// DeleteOrder deletes an order
// @Summary Delete an order
// @Description Delete an order by its ID
// @Tags Orders
// @Security BearerAuth
// @Param id path int true "Order ID"
// @Success 200 {object} map[string]string "Order deleted successfully"
// @Failure 400 {object} map[string]string "Invalid order ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Order not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/orders/{id} [delete]
func DeleteOrder(c *gin.Context) {
	workspaceID := c.MustGet("workspaceID").(uint)
	orderID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	result := database.DB.Where("id = ? AND workspace_id = ?", orderID, workspaceID).Delete(&models.Order{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete order"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}

	dispatchOrderNotification(services.OrderEventCancelled, uint(orderID), workspaceID)

	c.JSON(http.StatusOK, gin.H{"message": "Order deleted successfully"})
}

func dispatchOrderNotification(eventType string, orderID uint, workspaceID uint) {
	notification := services.OrderNotification{Type: eventType, OrderID: orderID, WorkspaceID: workspaceID}
	go services.NotifyOrderEvent(context.Background(), notification)
}

func orderNotificationType(status string) string {
	if status == constants.OrderStatusCanceled {
		return services.OrderEventCancelled
	}
	return services.OrderEventStatusUpdated
}

func isStatusTransition(previousStatus string, nextStatus string) bool {
	return previousStatus != nextStatus
}
