package controllers

import (
	"context"
	"errors"
	"log"
	"mobile-backend-go/constants"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
// @Param order body costedOrderInput true "Order data; calculated cost_price is an assertion"
// @Success 201 {object} models.Order
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 409 {object} map[string]string "Cost changed or calculation incomplete"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/orders [post]
func AddOrder(c *gin.Context) {
	addCostedOrder(c)
}

// UpdateOrder updates an order
// @Summary Update an order
// @Description Update an order's details
// @Tags Orders
// @Security BearerAuth
// @Accept  json
// @Produce  json
// @Param id path int true "Order ID"
// @Param order body costedOrderInput true "Order data; preserve saved item IDs"
// @Success 200 {object} map[string]string "Order updated successfully"
// @Failure 400 {object} map[string]string "Invalid order ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Order not found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Failure 409 {object} map[string]string "Cost assertion or historical identity conflict"
// @Router /api/orders/{id} [put]
func UpdateOrder(c *gin.Context) {
	updateCostedOrder(c)
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
