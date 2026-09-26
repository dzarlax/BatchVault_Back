package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"mobile-backend-go/models"
)

type recordedPush struct {
	deviceToken string
	payload     APNSPayload
}

type fakeAPNSDelivery struct {
	environment string
	errors      map[string]error
	sent        []recordedPush
	onSend      func(string)
	mu          sync.Mutex
}

func (delivery *fakeAPNSDelivery) Enabled() bool {
	return true
}

func (delivery *fakeAPNSDelivery) Environment() string {
	return delivery.environment
}

func (delivery *fakeAPNSDelivery) Send(_ context.Context, deviceToken string, payload APNSPayload) error {
	if delivery.onSend != nil {
		delivery.onSend(deviceToken)
	}
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	delivery.sent = append(delivery.sent, recordedPush{deviceToken: deviceToken, payload: payload})
	return delivery.errors[deviceToken]
}

func setupOrderNotificationTest(t *testing.T) (*gorm.DB, models.Workspace, models.User, models.User, models.User) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}, &models.PushDevice{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	users := []models.User{
		{Username: "actor-notification-user", Password: "hashed"},
		{Username: "member-notification-user", Password: "hashed"},
		{Username: "outside-notification-user", Password: "hashed"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	workspace := models.Workspace{Name: "Notifications", Slug: "notifications-test"}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	members := []models.WorkspaceMember{
		{WorkspaceID: workspace.ID, UserID: users[0].ID, Role: "owner"},
		{WorkspaceID: workspace.ID, UserID: users[1].ID, Role: "member"},
	}
	if err := db.Create(&members).Error; err != nil {
		t.Fatalf("create workspace members: %v", err)
	}

	return db, workspace, users[0], users[1], users[2]
}

func TestOrderNotifierFansOutToMemberDevicesIncludingActor(t *testing.T) {
	db, workspace, actor, member, outsideUser := setupOrderNotificationTest(t)
	devices := []models.PushDevice{
		{UserID: actor.ID, InstallationID: "actor-one", DeviceToken: "actor-device-one", Platform: "ios", Environment: "development", Enabled: true},
		{UserID: actor.ID, InstallationID: "actor-two", DeviceToken: "actor-device-two", Platform: "ios", Environment: "development", Enabled: true},
		{UserID: member.ID, InstallationID: "member-one", DeviceToken: "member-device", Platform: "ios", Environment: "development", Enabled: true},
		{UserID: member.ID, InstallationID: "member-disabled", DeviceToken: "disabled-device", Platform: "ios", Environment: "development", Enabled: false},
		{UserID: member.ID, InstallationID: "member-production", DeviceToken: "production-device", Platform: "ios", Environment: "production", Enabled: true},
		{UserID: outsideUser.ID, InstallationID: "outside-one", DeviceToken: "outside-device", Platform: "ios", Environment: "development", Enabled: true},
	}
	if err := db.Create(&devices).Error; err != nil {
		t.Fatalf("create push devices: %v", err)
	}
	if err := db.Model(&models.PushDevice{}).
		Where("installation_id = ?", "member-disabled").
		Update("enabled", false).Error; err != nil {
		t.Fatalf("disable fixture push device: %v", err)
	}

	delivery := &fakeAPNSDelivery{environment: "development"}
	notifier := NewOrderNotifier(db, delivery)
	notification := OrderNotification{Type: OrderEventCreated, OrderID: 42, WorkspaceID: workspace.ID}
	if err := notifier.Notify(context.Background(), notification); err != nil {
		t.Fatalf("notify: %v", err)
	}

	if len(delivery.sent) != 3 {
		t.Fatalf("delivery count = %d, want 3", len(delivery.sent))
	}
	seenDeviceTokens := make(map[string]bool, len(delivery.sent))
	for _, sent := range delivery.sent {
		seenDeviceTokens[sent.deviceToken] = true
		if sent.payload.Type != OrderEventCreated || sent.payload.OrderID != 42 || sent.payload.WorkspaceID != workspace.ID {
			t.Fatalf("notification payload does not contain the expected event identifiers")
		}
		if sent.payload.APS.Alert.Title != "BatchVault" || sent.payload.APS.Alert.Body != "A new order was created." || sent.payload.APS.Sound != "default" {
			t.Fatalf("notification payload contains an unexpected summary")
		}
	}
	if !seenDeviceTokens["actor-device-one"] || !seenDeviceTokens["actor-device-two"] || !seenDeviceTokens["member-device"] {
		t.Fatalf("active member devices, including the actor devices, were not all notified")
	}
}

func TestOrderNotifierDisablesInvalidDeviceToken(t *testing.T) {
	db, workspace, actor, _, _ := setupOrderNotificationTest(t)
	device := models.PushDevice{UserID: actor.ID, DeviceToken: "stale-device", Platform: "ios", Environment: "development", Enabled: true}
	if err := db.Create(&device).Error; err != nil {
		t.Fatalf("create push device: %v", err)
	}

	delivery := &fakeAPNSDelivery{
		environment: "development",
		errors:      map[string]error{"stale-device": &APNSError{StatusCode: 410, Reason: "Unregistered"}},
	}
	notifier := NewOrderNotifier(db, delivery)
	if err := notifier.Notify(context.Background(), OrderNotification{Type: OrderEventStatusUpdated, OrderID: 7, WorkspaceID: workspace.ID}); err != nil {
		t.Fatalf("notify: %v", err)
	}

	var stored models.PushDevice
	if err := db.First(&stored, device.ID).Error; err != nil {
		t.Fatalf("load push device: %v", err)
	}
	if stored.Enabled {
		t.Fatalf("invalid push device remained enabled")
	}
}

func TestOrderNotifierDoesNotDisableReregisteredDevice(t *testing.T) {
	db, workspace, actor, _, _ := setupOrderNotificationTest(t)
	device := models.PushDevice{UserID: actor.ID, DeviceToken: "reregistered-device", Platform: "ios", Environment: "development", Enabled: true, LastSeenAt: time.Now().Add(-time.Minute)}
	if err := db.Create(&device).Error; err != nil {
		t.Fatalf("create push device: %v", err)
	}

	var reRegistrationErr error
	delivery := &fakeAPNSDelivery{
		environment: "development",
		errors:      map[string]error{"reregistered-device": &APNSError{StatusCode: 410, Reason: "Unregistered"}},
		onSend: func(string) {
			reRegistrationErr = db.Model(&models.PushDevice{}).Where("id = ?", device.ID).Updates(map[string]interface{}{
				"environment":  "production",
				"last_seen_at": time.Now().UTC(),
				"enabled":      true,
			}).Error
		},
	}
	if err := NewOrderNotifier(db, delivery).Notify(context.Background(), OrderNotification{Type: OrderEventStatusUpdated, OrderID: 8, WorkspaceID: workspace.ID}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if reRegistrationErr != nil {
		t.Fatalf("re-register push device: %v", reRegistrationErr)
	}

	var stored models.PushDevice
	if err := db.First(&stored, device.ID).Error; err != nil {
		t.Fatalf("load push device: %v", err)
	}
	if !stored.Enabled || stored.Environment != "production" {
		t.Fatalf("new device registration was disabled by stale APNs cleanup")
	}
}

func TestConfigureAPNSFromEnvironmentReloadsDefaultDelivery(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate APNs test key: %v", err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal APNs test key: %v", err)
	}
	t.Setenv("APNS_AUTH_KEY_BASE64", base64.StdEncoding.EncodeToString(encodedKey))
	t.Setenv("APNS_KEY_ID", "test-key-id")
	t.Setenv("APNS_TEAM_ID", "test-team-id")
	t.Setenv("APNS_TOPIC", "com.example.batchvault")
	t.Setenv("APNS_ENVIRONMENT", "development")
	t.Cleanup(func() {
		defaultAPNSDeliveryMu.Lock()
		defer defaultAPNSDeliveryMu.Unlock()
		defaultAPNSDelivery = &APNSClient{}
	})

	ConfigureAPNSFromEnvironment()
	delivery := currentAPNSDelivery()
	if !delivery.Enabled() || delivery.Environment() != "development" {
		t.Fatalf("default APNs delivery was not reloaded from the environment")
	}
}
