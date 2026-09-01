package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

const (
	OrderEventCreated        = "order_created"
	OrderEventStatusUpdated  = "order_status_updated"
	OrderEventCancelled      = "order_cancelled"
	maxConcurrentDeliveries  = 8
	orderNotificationTimeout = 15 * time.Second
)

// OrderNotification identifies the persisted order event that should be delivered.
type OrderNotification struct {
	Type        string
	OrderID     uint
	WorkspaceID uint
}

// OrderNotifier finds active workspace member devices and dispatches notifications.
type OrderNotifier struct {
	db       *gorm.DB
	delivery APNSDelivery
}

// NewOrderNotifier constructs a notifier with an explicit database and delivery boundary.
func NewOrderNotifier(db *gorm.DB, delivery APNSDelivery) *OrderNotifier {
	return &OrderNotifier{db: db, delivery: delivery}
}

var (
	defaultAPNSDeliveryMu sync.RWMutex
	defaultAPNSDelivery   APNSDelivery = &APNSClient{}
)

// ConfigureAPNSFromEnvironment reloads the APNs client after environment configuration is available.
func ConfigureAPNSFromEnvironment() {
	defaultAPNSDeliveryMu.Lock()
	defer defaultAPNSDeliveryMu.Unlock()
	defaultAPNSDelivery = NewAPNSClientFromEnvironment()
}

// NotifyOrderEvent attempts delivery after an order transaction has already succeeded.
// Delivery failures are logged without device tokens and do not affect the caller's response.
func NotifyOrderEvent(ctx context.Context, notification OrderNotification) {
	notifier := NewOrderNotifier(database.DB, currentAPNSDelivery())
	if err := notifier.Notify(ctx, notification); err != nil {
		log.Printf("Push notification dispatch failed for order %d in workspace %d", notification.OrderID, notification.WorkspaceID)
	}
}

func currentAPNSDelivery() APNSDelivery {
	defaultAPNSDeliveryMu.RLock()
	defer defaultAPNSDeliveryMu.RUnlock()
	return defaultAPNSDelivery
}

// Notify sends an event to every enabled device belonging to a workspace member, including the actor.
func (notifier *OrderNotifier) Notify(ctx context.Context, notification OrderNotification) error {
	if notifier == nil || notifier.delivery == nil || !notifier.delivery.Enabled() {
		return nil
	}
	if notifier.db == nil {
		return fmt.Errorf("push notification database is unavailable")
	}

	var devices []models.PushDevice
	if err := notifier.db.WithContext(ctx).
		Model(&models.PushDevice{}).
		Joins("JOIN workspace_members ON workspace_members.user_id = push_devices.user_id AND workspace_members.deleted_at IS NULL").
		Where("workspace_members.workspace_id = ? AND push_devices.enabled = ? AND push_devices.environment = ?", notification.WorkspaceID, true, notifier.delivery.Environment()).
		Find(&devices).Error; err != nil {
		return err
	}

	deliveryContext, cancel := context.WithTimeout(ctx, orderNotificationTimeout)
	defer cancel()
	payload := orderPayload(notification)
	jobs := make(chan models.PushDevice)
	var workers sync.WaitGroup
	workerCount := min(maxConcurrentDeliveries, len(devices))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for device := range jobs {
				notifier.deliverToDevice(deliveryContext, device, payload)
			}
		}()
	}
	defer func() {
		close(jobs)
		workers.Wait()
	}()

	for _, device := range devices {
		select {
		case <-deliveryContext.Done():
			return deliveryContext.Err()
		case jobs <- device:
		}
	}
	return nil
}

func (notifier *OrderNotifier) deliverToDevice(ctx context.Context, device models.PushDevice, payload APNSPayload) {
	err := notifier.delivery.Send(ctx, device.DeviceToken, payload)
	if IsInvalidDeviceTokenError(err) {
		if disableErr := notifier.db.WithContext(ctx).Model(&models.PushDevice{}).
			Where("id = ? AND user_id = ? AND environment = ? AND last_seen_at = ? AND enabled = ?", device.ID, device.UserID, device.Environment, device.LastSeenAt, true).
			Updates(map[string]interface{}{"enabled": false}).Error; disableErr != nil {
			log.Printf("Failed to disable stale push device %d", device.ID)
		}
		return
	}
	if err != nil {
		log.Printf("APNs delivery failed for push device %d", device.ID)
	}
}

func orderPayload(notification OrderNotification) APNSPayload {
	body := "An order was updated."
	switch notification.Type {
	case OrderEventCreated:
		body = "A new order was created."
	case OrderEventCancelled:
		body = "An order was cancelled."
	}
	return APNSPayload{
		APS: APNSAlertPayload{
			Alert: APNSAlert{Title: "BatchVault", Body: body},
			Sound: "default",
		},
		Type:        notification.Type,
		OrderID:     notification.OrderID,
		WorkspaceID: notification.WorkspaceID,
	}
}
