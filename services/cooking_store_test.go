package services

import (
	"encoding/json"
	"fmt"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func cookingStoreFixture(t *testing.T, forcePostgres bool) (CookingService, []models.Order) {
	t.Helper()
	var db *gorm.DB
	var e error
	raw := os.Getenv("COSTING_TEST_DATABASE_URL")
	if forcePostgres && raw == "" {
		t.Skip("COSTING_TEST_DATABASE_URL required for PostgreSQL concurrency")
	}
	if raw != "" {
		u, e := url.Parse(raw)
		if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Path != "/costing_test" {
			t.Fatal("Only disposable localhost costing_test is permitted")
		}
		admin, e := gorm.Open(postgres.Open(raw), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if e != nil {
			t.Fatal(e)
		}
		schema := "cooking_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if e = admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); sql, _ := admin.DB(); sql.Close() })
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		db, e = gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	} else {
		db, e = gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if raw == "" {
		sqlDB.SetMaxOpenConns(1)
	}
	if e = db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}, &models.Ingredient{}, &models.WorkspaceIngredient{}, &models.Price{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.Package{}, &models.Product{}, &models.ProductOption{}, &models.ProductCostComponent{}, &models.ProductAssortmentComponent{}, &models.Client{}, &models.Order{}, &models.OrderItem{}, &models.CookingSession{}, &models.CookingSessionIngredient{}); e != nil {
		t.Fatal(e)
	}
	if e = database.MigrateCooking(db); e != nil {
		t.Fatal(e)
	}
	if e = database.MigrateCooking(db); e != nil {
		t.Fatal(e)
	}
	user := models.User{ID: 1, Username: "cooking-test", Password: "unused"}
	ws := models.Workspace{ID: 1, Name: "Cooking test", Slug: "cooking-test"}
	client := models.Client{ID: 1, Name: "Synthetic", Surname: "Customer", WorkspaceID: &ws.ID, UserID: user.ID}
	for _, row := range []any{&user, &ws, &client} {
		if e = db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
	orders, products, recipes := cookingFixture()
	for ri := range recipes {
		r := &recipes[ri]
		r.WorkspaceID = &ws.ID
		r.UserID = user.ID
		for i := range r.RecipeIngredients {
			row := &r.RecipeIngredients[i]
			row.RecipeID = r.ID
			row.Ingredient.Name = fmt.Sprintf("%s-%d", row.Ingredient.Name, row.IngredientID)
			if e = db.Omit("RecipeIngredients", "Prices", "CookingSessionIngredients", "WorkspaceIngredients").Create(&row.Ingredient).Error; e != nil {
				t.Fatal(e)
			}
			if e = db.Create(&models.WorkspaceIngredient{WorkspaceID: ws.ID, IngredientID: row.IngredientID, Active: true}).Error; e != nil {
				t.Fatal(e)
			}
		}
		if e = db.Omit("RecipeIngredients", "CookingSessions", "ProductOptions", "Workspace", "User").Create(r).Error; e != nil {
			t.Fatal(e)
		}
		for _, row := range r.RecipeIngredients {
			if e = db.Omit("Ingredient", "Recipe").Create(&row).Error; e != nil {
				t.Fatal(e)
			}
		}
	}
	for i := range products {
		product := &products[i]
		product.WorkspaceID = &ws.ID
		product.UserID = user.ID
		product.CostMode = "manual"
		product.Cost = 1
		product.Price = 10
		product.Package.Name = product.Name + " pack"
		product.Package.WorkspaceID = &ws.ID
		product.Package.UserID = user.ID
		if e = db.Omit("Workspace", "User").Create(&product.Package).Error; e != nil {
			t.Fatal(e)
		}
		product.PackageID = product.Package.ID
		if e = db.Omit("Package", "Workspace", "User", "AssortmentComponents", "CostComponents", "Options").Create(product).Error; e != nil {
			t.Fatal(e)
		}
		for _, comp := range product.AssortmentComponents {
			comp.WorkspaceID = ws.ID
			comp.ProductID = product.ID
			if e = db.Omit("ComponentProduct").Create(&comp).Error; e != nil {
				t.Fatal(e)
			}
		}
	}
	for i := range orders {
		order := &orders[i]
		order.Status = "new"
		order.WorkspaceID = &ws.ID
		order.UserID = user.ID
		order.ClientID = client.ID
		order.Date = time.Now()
		if e = db.Omit("Items", "Client", "Workspace", "User").Create(order).Error; e != nil {
			t.Fatal(e)
		}
		for _, line := range order.Items {
			line.OrderID = order.ID
			line.Price = 10
			line.Cost_price = 1
			if e = db.Omit("Order", "Product").Create(&line).Error; e != nil {
				t.Fatal(e)
			}
		}
	}
	return CookingService{DB: db, WorkspaceID: ws.ID, UserID: user.ID}, orders
}
func storeRun(t *testing.T, s CookingService) models.CookingRun {
	t.Helper()
	input := models.CookingPlanInput{OrderIDs: []uint{104, 105, 106}}
	preview, e := s.Plan(input)
	if e != nil {
		t.Fatal(e)
	}
	request := models.CookingCreateInput{CookingPlanInput: input, Title: "Synthetic cooking", Date: "2026-11-07", SourceSignature: preview.SourceSignature, StockVersion: preview.StockVersion, OperationID: uuid.NewString()}
	body, e := s.Create(request)
	if e != nil {
		t.Fatal(e)
	}
	var run models.CookingRun
	if e = json.Unmarshal(body, &run); e != nil {
		t.Fatal(e)
	}
	again, e := s.Create(request)
	if e != nil || !equalCookingJSON(again, body) {
		t.Fatalf("create replay changed: %s %v", again, e)
	}
	request.Title = "Changed operation"
	if _, e = s.Create(request); e == nil {
		t.Fatal("operation payload reuse accepted")
	}
	prep := models.CookingPreparationInput{RunID: &run.ID, PreparedMeatG: "2900", Flavors: []models.CookingPreparationFlavorInput{{RecipeID: 1, ActualMeatG: "1500"}, {RecipeID: 2, ActualMeatG: "700"}, {RecipeID: 3, ActualMeatG: "700"}}, SourceSignature: run.SourceSignature, Version: run.Version, OperationID: uuid.NewString()}
	body, e = s.Prepare(run.ID, prep)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(body, &run); e != nil {
		t.Fatal(e)
	}
	if run.State != "prepared" || run.Preparation.AllocatedMeatG != "2900.000" {
		t.Fatalf("preparation not persisted: %+v", run)
	}
	return run
}
func outputFixture() []models.CookingComponent {
	return []models.CookingComponent{{RecipeID: 1, QuantityG: "730"}, {RecipeID: 2, QuantityG: "330"}, {RecipeID: 3, QuantityG: "340"}}
}
func decodeStoreRun(t *testing.T, body []byte, e error) models.CookingRun {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
	var run models.CookingRun
	if e = json.Unmarshal(body, &run); e != nil {
		t.Fatal(e)
	}
	return run
}
func assertBulk(t *testing.T, s CookingService, want map[uint]string) {
	t.Helper()
	stock, e := s.Stock()
	if e != nil {
		t.Fatal(e)
	}
	got := map[uint]string{}
	for _, lot := range stock.Lots {
		if lot.Kind == "bulk" {
			v := got[lot.RecipeID]
			if v == "" {
				v = "0"
			}
			got[lot.RecipeID], e = addStock(v, lot.Quantity)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	for id, n := range want {
		if got[id] != n {
			t.Fatalf("bulk recipe %d = %s, want %s", id, got[id], n)
		}
	}
}
func TestCookingStorePartialPackingReplayAndIssue(t *testing.T) {
	s, _ := cookingStoreFixture(t, false)
	run := storeRun(t, s)
	invalid := models.CookingResultInput{Version: run.Version, StockVersion: run.StockVersion, OperationID: uuid.NewString(), ActualUsableG: outputFixture(), Packing: &models.CookingPackingInput{Lines: []models.CookingPackingLineInput{{ProductID: 101, Quantity: 100}}}}
	if _, e := s.Results(run.ID, invalid); e == nil {
		t.Fatal("over-packing accepted")
	}
	var count int64
	s.DB.Model(&models.CookingStockLot{}).Count(&count)
	if count != 0 {
		t.Fatal("failed result left partial stock")
	}
	body, e := s.Results(run.ID, models.CookingResultInput{Version: run.Version, StockVersion: run.StockVersion, OperationID: uuid.NewString(), ActualUsableG: outputFixture()})
	run = decodeStoreRun(t, body, e)
	if run.State != "cooked" {
		t.Fatal(run.State)
	}
	first := models.CookingPackingInput{Version: run.Version, StockVersion: run.StockVersion, OperationID: uuid.NewString(), Lines: []models.CookingPackingLineInput{{ProductID: 101, Quantity: 4}}}
	body, e = s.Pack(run.ID, first)
	run = decodeStoreRun(t, body, e)
	assertBulk(t, s, map[uint]string{1: "330.000", 2: "330.000", 3: "340.000"})
	repeated, e := s.Pack(run.ID, first)
	if e != nil || !equalCookingJSON(repeated, body) {
		t.Fatal("packing replay changed", e)
	}
	// A later catalog change cannot rewrite the frozen 100g packaging specification.
	if e = s.DB.Model(&models.Package{}).Where("id = ?", 1).Update("net_weight_g", "999").Error; e != nil {
		t.Fatal(e)
	}
	rest := models.CookingPackingInput{Version: run.Version, StockVersion: run.StockVersion, OperationID: uuid.NewString(), Lines: []models.CookingPackingLineInput{{ProductID: 101, Quantity: 2}, {ProductID: 102, Quantity: 2}, {ProductID: 103, Quantity: 2}, {ProductID: 104, Quantity: 2}}}
	body, e = s.Pack(run.ID, rest)
	run = decodeStoreRun(t, body, e)
	assertBulk(t, s, map[uint]string{1: "30.000", 2: "30.000", 3: "40.000"})
	for _, id := range []uint{104, 105, 106} {
		e = CookingOrderTransaction(s.DB, s.WorkspaceID, func(tx *gorm.DB) error {
			if e := tx.Model(&models.Order{}).Where("id = ?", id).Update("status", "finished").Error; e != nil {
				return e
			}
			return SyncCookingOrder(tx, s.WorkspaceID, s.UserID, id, "finished", false)
		})
		if e != nil {
			t.Fatal(e)
		}
		e = CookingOrderTransaction(s.DB, s.WorkspaceID, func(tx *gorm.DB) error { return SyncCookingOrder(tx, s.WorkspaceID, s.UserID, id, "finished", false) })
		if e != nil {
			t.Fatal(e)
		}
	}
	stock, e := s.Stock()
	if e != nil {
		t.Fatal(e)
	}
	for _, lot := range stock.Lots {
		if lot.Kind == "packaged" && lot.Quantity != "0" {
			n, _ := stockNumber(lot.Quantity)
			if n.Sign() != 0 {
				t.Fatalf("issued packages remain: %+v", lot)
			}
		}
	}
	assertBulk(t, s, map[uint]string{1: "30.000", 2: "30.000", 3: "40.000"})
}
func TestCookingStoreSourceChangeIsolationAndReservations(t *testing.T) {
	s, _ := cookingStoreFixture(t, false)
	input := models.CookingPlanInput{OrderIDs: []uint{104, 105, 106}}
	preview, e := s.Plan(input)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DB.Model(&models.RecipeIngredient{}).Where("id = ?", 12).Update("quantity", "21").Error; e != nil {
		t.Fatal(e)
	}
	_, e = s.Create(models.CookingCreateInput{CookingPlanInput: input, SourceSignature: preview.SourceSignature, StockVersion: preview.StockVersion, OperationID: uuid.NewString()})
	if e == nil {
		t.Fatal("stale recipe source accepted")
	}
	other := s
	other.WorkspaceID = 2
	if _, e = other.Plan(models.CookingPlanInput{OrderIDs: input.OrderIDs}); e == nil {
		t.Fatal("cross-workspace order accepted")
	}
	stock, e := s.Stock()
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Adjust(models.CookingAdjustmentInput{Kind: "initial", RecipeID: 1, Quantity: "50", Reason: "Synthetic opening stock", StockVersion: stock.Version, OperationID: uuid.NewString()})
	if e != nil {
		t.Fatal(e)
	}
	stock, _ = s.Stock()
	lot := stock.Lots[0]
	input.StockUses = []models.CookingStockUse{{LotID: lot.ID, Quantity: "50"}}
	preview, e = s.Plan(input)
	if e != nil {
		t.Fatal(e)
	}
	if preview.Plan.Flavors[0].TargetFinishedG != "650.000" {
		t.Fatal("bulk stock not deducted", preview.Plan.Flavors[0])
	}
	req := models.CookingCreateInput{CookingPlanInput: input, SourceSignature: preview.SourceSignature, StockVersion: preview.StockVersion, OperationID: uuid.NewString()}
	body, e := s.Create(req)
	run := decodeStoreRun(t, body, e)
	if _, e = other.Run(run.ID); e == nil {
		t.Fatal("cross-workspace run visible")
	}
	req.OperationID = uuid.NewString()
	if _, e = s.Create(req); e == nil {
		t.Fatal("stale stock double-reserved")
	}
	body, e = s.Archive(run.ID, models.CookingArchiveInput{Version: run.Version, OperationID: uuid.NewString()})
	_ = decodeStoreRun(t, body, e)
	stock, _ = s.Stock()
	if stock.Lots[0].Available != "50.000" {
		t.Fatal("archive lost stock", stock)
	}
}
func TestCookingPostgresConcurrentPacking(t *testing.T) {
	s, _ := cookingStoreFixture(t, true)
	run := storeRun(t, s)
	body, e := s.Results(run.ID, models.CookingResultInput{Version: run.Version, StockVersion: run.StockVersion, OperationID: uuid.NewString(), ActualUsableG: outputFixture()})
	run = decodeStoreRun(t, body, e)
	requests := []models.CookingPackingInput{
		{Version: run.Version, StockVersion: run.StockVersion, OperationID: uuid.NewString(), Lines: []models.CookingPackingLineInput{{ProductID: 101, Quantity: 7}}},
		{Version: run.Version, StockVersion: run.StockVersion, OperationID: uuid.NewString(), Lines: []models.CookingPackingLineInput{{ProductID: 101, Quantity: 7}}},
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range requests {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, errs[i] = s.Pack(run.ID, requests[i]) }(i)
	}
	wg.Wait()
	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected one packing commit, got %d: %v", winners, errs)
	}
	assertBulk(t, s, map[uint]string{1: "30.000", 2: "330.000", 3: "340.000"})
}

func equalCookingJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
