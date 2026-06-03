package demoseed

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"mobile-backend-go/constants"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

func setupTestDB(t *testing.T) *gorm.DB {
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

	if err := db.AutoMigrate(
		&models.User{},
		&models.Workspace{},
		&models.WorkspaceMember{},
		&models.Ingredient{},
		&models.WorkspaceIngredient{},
		&models.Price{},
		&models.Recipe{},
		&models.RecipeIngredient{},
		&models.Package{},
		&models.Product{},
		&models.ProductOption{},
		&models.Client{},
		&models.Order{},
		&models.OrderItem{},
		&models.CookingSession{},
		&models.CookingSessionIngredient{},
		&DemoSeedMarker{},
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	database.DB = db
	return db
}

func TestRunRequiresConfirmation(t *testing.T) {
	db := setupTestDB(t)

	if _, err := Run(db, Options{}); err == nil {
		t.Fatal("Run without confirmation succeeded, want error")
	}
}

func TestRefreshIsIdempotentAndUpdatesMarkers(t *testing.T) {
	db := setupTestDB(t)

	first, err := Run(db, Options{Confirm: ConfirmValue, Mode: ModeRefresh})
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if len(first.Accounts) != len(accountFixtures) {
		t.Fatalf("seeded account count = %d, want %d", len(first.Accounts), len(accountFixtures))
	}

	countsAfterFirst := collectCounts(t, db)
	second, err := Run(db, Options{Confirm: ConfirmValue, Mode: ModeRefresh})
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	countsAfterSecond := collectCounts(t, db)

	if countsAfterSecond != countsAfterFirst {
		t.Fatalf("counts after second refresh = %+v, want %+v", countsAfterSecond, countsAfterFirst)
	}
	for i := range first.Accounts {
		if second.Accounts[i].WorkspaceID != first.Accounts[i].WorkspaceID {
			t.Fatalf("workspace changed for %s: got %d want %d", first.Accounts[i].Username, second.Accounts[i].WorkspaceID, first.Accounts[i].WorkspaceID)
		}
	}

	var markers []DemoSeedMarker
	if err := db.Find(&markers).Error; err != nil {
		t.Fatalf("load markers: %v", err)
	}
	if len(markers) != len(accountFixtures) {
		t.Fatalf("marker count = %d, want %d", len(markers), len(accountFixtures))
	}
	for _, marker := range markers {
		if marker.DatasetVersion != DatasetVersion || marker.LastMode != string(ModeRefresh) {
			t.Fatalf("marker = %+v, want version %s mode %s", marker, DatasetVersion, ModeRefresh)
		}
	}
}

func TestResetRecreatesDemoAccounts(t *testing.T) {
	db := setupTestDB(t)

	first, err := Run(db, Options{Confirm: ConfirmValue, Mode: ModeRefresh})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	second, err := Run(db, Options{Confirm: ConfirmValue, Mode: ModeReset})
	if err != nil {
		t.Fatalf("reset: %v", err)
	}

	for i := range first.Accounts {
		if second.Accounts[i].WorkspaceID == first.Accounts[i].WorkspaceID {
			t.Fatalf("reset preserved workspace for %s, want recreated workspace", first.Accounts[i].Username)
		}
		if !second.Accounts[i].Created {
			t.Fatalf("reset did not report recreated account for %s", second.Accounts[i].Username)
		}
	}
}

func TestRunDoesNotTouchNonDemoUserData(t *testing.T) {
	db := setupTestDB(t)

	user := models.User{Username: "real-user", Password: "hashed"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create real user: %v", err)
	}
	userID := user.ID
	workspace := models.Workspace{Name: "Real workspace", Slug: "real-workspace", PersonalUserID: &userID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create real workspace: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: constants.WorkspaceRoleOwner}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create real membership: %v", err)
	}
	client := models.Client{Name: "Real", Surname: "Customer", UserID: user.ID, WorkspaceID: &workspace.ID}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create real client: %v", err)
	}

	if _, err := Run(db, Options{Confirm: ConfirmValue, Mode: ModeReset}); err != nil {
		t.Fatalf("run reset: %v", err)
	}

	var loaded models.Client
	if err := db.First(&loaded, client.ID).Error; err != nil {
		t.Fatalf("real client was modified or deleted: %v", err)
	}
	if loaded.Name != client.Name || loaded.UserID != user.ID {
		t.Fatalf("real client = %+v, want %+v", loaded, client)
	}
}

type seedCounts struct {
	Users       int64
	Workspaces  int64
	Ingredients int64
	Prices      int64
	Recipes     int64
	Products    int64
	Clients     int64
	Orders      int64
	Sessions    int64
	Markers     int64
}

func collectCounts(t *testing.T, db *gorm.DB) seedCounts {
	t.Helper()

	var counts seedCounts
	mustCount(t, db, &models.User{}, &counts.Users)
	mustCount(t, db, &models.Workspace{}, &counts.Workspaces)
	mustCount(t, db, &models.Ingredient{}, &counts.Ingredients)
	mustCount(t, db, &models.Price{}, &counts.Prices)
	mustCount(t, db, &models.Recipe{}, &counts.Recipes)
	mustCount(t, db, &models.Product{}, &counts.Products)
	mustCount(t, db, &models.Client{}, &counts.Clients)
	mustCount(t, db, &models.Order{}, &counts.Orders)
	mustCount(t, db, &models.CookingSession{}, &counts.Sessions)
	mustCount(t, db, &DemoSeedMarker{}, &counts.Markers)
	return counts
}

func mustCount(t *testing.T, db *gorm.DB, model any, target *int64) {
	t.Helper()
	if err := db.Model(model).Count(target).Error; err != nil {
		t.Fatalf("count %T: %v", model, err)
	}
}
