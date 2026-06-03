package demoseed

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/utils"
)

type Mode string

const (
	ModeRefresh Mode = "refresh"
	ModeReset   Mode = "reset"
)

type Options struct {
	Confirm   string
	Mode      Mode
	Passwords map[string]string
}

type Result struct {
	DatasetVersion string
	Mode           Mode
	Accounts       []AccountResult
}

type AccountResult struct {
	Username    string
	WorkspaceID uint
	Created     bool
}

type DemoSeedMarker struct {
	ID             uint `gorm:"primaryKey"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Username       string `gorm:"uniqueIndex;not null"`
	WorkspaceID    uint   `gorm:"not null"`
	DatasetVersion string `gorm:"not null"`
	LastMode       string `gorm:"not null"`
}

func Run(db *gorm.DB, opts Options) (Result, error) {
	if opts.Confirm != ConfirmValue {
		return Result{}, fmt.Errorf("refusing to seed demo accounts without DEMO_SEED_CONFIRM=%s", ConfirmValue)
	}

	mode := opts.Mode
	if mode == "" {
		mode = ModeRefresh
	}
	if mode != ModeRefresh && mode != ModeReset {
		return Result{}, fmt.Errorf("unsupported demo seed mode %q", mode)
	}

	if err := validateFixtures(); err != nil {
		return Result{}, err
	}
	if err := db.AutoMigrate(&DemoSeedMarker{}); err != nil {
		return Result{}, fmt.Errorf("migrate demo seed marker: %w", err)
	}

	result := Result{DatasetVersion: DatasetVersion, Mode: mode}
	for _, fixture := range accountFixtures {
		accountResult, err := seedAccount(db, fixture, opts, mode)
		if err != nil {
			return Result{}, fmt.Errorf("seed %s: %w", fixture.Username, err)
		}
		result.Accounts = append(result.Accounts, accountResult)
	}

	return result, nil
}

func validateFixtures() error {
	for _, fixture := range accountFixtures {
		if !strings.HasPrefix(fixture.Username, "demo-") {
			return fmt.Errorf("demo username %q must start with demo-", fixture.Username)
		}
		if fixture.Username == "" || fixture.WorkspaceName == "" {
			return fmt.Errorf("demo fixture has empty username or workspace name")
		}
		if err := validateUniqueKeys("ingredient", fixture.Username, ingredientKeys(fixture.Ingredients)); err != nil {
			return err
		}
		if err := validateUniqueKeys("recipe", fixture.Username, recipeKeys(fixture.Recipes)); err != nil {
			return err
		}
		if err := validateUniqueKeys("package", fixture.Username, packageKeys(fixture.Packages)); err != nil {
			return err
		}
		if err := validateUniqueKeys("product", fixture.Username, productKeys(fixture.Products)); err != nil {
			return err
		}
		if err := validateUniqueKeys("client", fixture.Username, clientKeys(fixture.Clients)); err != nil {
			return err
		}
	}
	return nil
}

func seedAccount(db *gorm.DB, fixture accountFixture, opts Options, mode Mode) (AccountResult, error) {
	var accountResult AccountResult

	err := db.Transaction(func(tx *gorm.DB) error {
		if mode == ModeReset {
			if err := deleteDemoAccount(tx, fixture.Username); err != nil {
				return err
			}
		}

		user, created, err := ensureDemoUser(tx, fixture, opts)
		if err != nil {
			return err
		}

		member, err := database.EnsurePersonalWorkspaceForUser(tx, user.ID)
		if err != nil {
			return err
		}
		workspace := member.Workspace
		if err := tx.Model(&models.Workspace{}).
			Where("id = ?", workspace.ID).
			Updates(map[string]interface{}{
				"name": fixture.WorkspaceName,
				"slug": fixture.Username,
			}).Error; err != nil {
			return err
		}

		if mode == ModeRefresh {
			if err := deleteOperationalData(tx, user.ID, workspace.ID); err != nil {
				return err
			}
		}

		if err := seedOperationalData(tx, user.ID, workspace.ID, fixture); err != nil {
			return err
		}
		if err := upsertMarker(tx, fixture.Username, workspace.ID, mode); err != nil {
			return err
		}

		accountResult = AccountResult{
			Username:    fixture.Username,
			WorkspaceID: workspace.ID,
			Created:     created,
		}
		return nil
	})

	return accountResult, err
}

func ensureDemoUser(tx *gorm.DB, fixture accountFixture, opts Options) (models.User, bool, error) {
	password := defaultDemoPassword
	if opts.Passwords != nil && opts.Passwords[fixture.PasswordEnv] != "" {
		password = opts.Passwords[fixture.PasswordEnv]
	}

	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		return models.User{}, false, fmt.Errorf("hash password: %w", err)
	}

	var user models.User
	err = tx.Where("username = ?", fixture.Username).First(&user).Error
	if err == nil {
		if err := tx.Model(&user).Updates(map[string]interface{}{
			"password":      hashedPassword,
			"token_version": gorm.Expr("token_version + ?", 1),
		}).Error; err != nil {
			return models.User{}, false, err
		}
		return user, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.User{}, false, err
	}

	user = models.User{Username: fixture.Username, Password: hashedPassword}
	if err := tx.Create(&user).Error; err != nil {
		return models.User{}, false, err
	}
	return user, true, nil
}

func seedOperationalData(tx *gorm.DB, userID uint, workspaceID uint, fixture accountFixture) error {
	ingredientIDs, err := seedIngredients(tx, workspaceID, fixture.Ingredients)
	if err != nil {
		return err
	}

	for _, ingredient := range fixture.Ingredients {
		ingredientID := ingredientIDs[ingredient.Key]
		for _, price := range ingredient.Prices {
			row := models.Price{
				IngredientID: ingredientID,
				Price:        price.Price,
				Quantity:     price.Quantity,
				Unit:         price.Unit,
				Date:         baseDate.AddDate(0, 0, -price.DaysAgo),
				UserID:       userID,
				WorkspaceID:  &workspaceID,
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("create price for %s: %w", ingredient.Key, err)
			}
		}
	}

	recipeIDs, err := seedRecipes(tx, userID, workspaceID, ingredientIDs, fixture.Recipes)
	if err != nil {
		return err
	}
	packageIDs, err := seedPackages(tx, userID, workspaceID, fixture.Packages)
	if err != nil {
		return err
	}
	productIDs, err := seedProducts(tx, userID, workspaceID, packageIDs, recipeIDs, fixture.Products)
	if err != nil {
		return err
	}
	clientIDs, err := seedClients(tx, userID, workspaceID, fixture.Clients)
	if err != nil {
		return err
	}
	if err := seedOrders(tx, userID, workspaceID, clientIDs, productIDs, fixture.Orders); err != nil {
		return err
	}
	if err := seedSessions(tx, userID, workspaceID, recipeIDs, ingredientIDs, fixture); err != nil {
		return err
	}

	return nil
}

func seedIngredients(tx *gorm.DB, workspaceID uint, fixtures []ingredientFixture) (map[string]uint, error) {
	ids := make(map[string]uint, len(fixtures))
	for _, fixture := range fixtures {
		ingredient := models.Ingredient{Name: fixture.Name}
		if err := tx.Where("name = ?", fixture.Name).
			Attrs(models.Ingredient{Type: fixture.Type}).
			FirstOrCreate(&ingredient).Error; err != nil {
			return nil, fmt.Errorf("create ingredient %s: %w", fixture.Key, err)
		}

		workspaceIngredient, err := database.EnsureWorkspaceIngredient(tx, workspaceID, ingredient.ID)
		if err != nil {
			return nil, fmt.Errorf("ensure workspace ingredient %s: %w", fixture.Key, err)
		}
		if err := tx.Model(&models.WorkspaceIngredient{}).
			Where("id = ?", workspaceIngredient.ID).
			Updates(map[string]interface{}{
				"active":   true,
				"alias":    fixture.Alias,
				"category": fixture.Category,
			}).Error; err != nil {
			return nil, fmt.Errorf("update workspace ingredient %s: %w", fixture.Key, err)
		}

		ids[fixture.Key] = ingredient.ID
	}
	return ids, nil
}

func seedRecipes(tx *gorm.DB, userID uint, workspaceID uint, ingredientIDs map[string]uint, fixtures []recipeFixture) (map[string]uint, error) {
	ids := make(map[string]uint, len(fixtures))
	for _, fixture := range fixtures {
		recipe := models.Recipe{Name: fixture.Name, UserID: userID, WorkspaceID: &workspaceID}
		if err := tx.Create(&recipe).Error; err != nil {
			return nil, fmt.Errorf("create recipe %s: %w", fixture.Key, err)
		}
		for _, ingredient := range fixture.Ingredients {
			ingredientID, ok := ingredientIDs[ingredient.IngredientKey]
			if !ok {
				return nil, fmt.Errorf("recipe %s references unknown ingredient %s", fixture.Key, ingredient.IngredientKey)
			}
			row := models.RecipeIngredient{
				RecipeID:     recipe.ID,
				IngredientID: ingredientID,
				Quantity:     ingredient.Quantity,
				Unit:         ingredient.Unit,
			}
			if err := tx.Create(&row).Error; err != nil {
				return nil, fmt.Errorf("create recipe ingredient %s/%s: %w", fixture.Key, ingredient.IngredientKey, err)
			}
		}
		ids[fixture.Key] = recipe.ID
	}
	return ids, nil
}

func seedPackages(tx *gorm.DB, userID uint, workspaceID uint, fixtures []packageFixture) (map[string]uint, error) {
	ids := make(map[string]uint, len(fixtures))
	for _, fixture := range fixtures {
		row := models.Package{Name: fixture.Name, UserID: userID, WorkspaceID: &workspaceID}
		if err := tx.Create(&row).Error; err != nil {
			return nil, fmt.Errorf("create package %s: %w", fixture.Key, err)
		}
		ids[fixture.Key] = row.ID
	}
	return ids, nil
}

func seedProducts(tx *gorm.DB, userID uint, workspaceID uint, packageIDs map[string]uint, recipeIDs map[string]uint, fixtures []productFixture) (map[string]uint, error) {
	ids := make(map[string]uint, len(fixtures))
	for _, fixture := range fixtures {
		packageID, ok := packageIDs[fixture.PackageKey]
		if !ok {
			return nil, fmt.Errorf("product %s references unknown package %s", fixture.Key, fixture.PackageKey)
		}
		row := models.Product{
			Name:        fixture.Name,
			Description: fixture.Description,
			Price:       fixture.Price,
			Cost:        fixture.Cost,
			UserID:      userID,
			WorkspaceID: &workspaceID,
			PackageID:   packageID,
		}
		if err := tx.Create(&row).Error; err != nil {
			return nil, fmt.Errorf("create product %s: %w", fixture.Key, err)
		}
		for _, recipeKey := range fixture.RecipeKeys {
			recipeID, ok := recipeIDs[recipeKey]
			if !ok {
				return nil, fmt.Errorf("product %s references unknown recipe %s", fixture.Key, recipeKey)
			}
			option := models.ProductOption{ProductID: row.ID, RecipeID: recipeID, UserID: userID}
			if err := tx.Create(&option).Error; err != nil {
				return nil, fmt.Errorf("create product option %s/%s: %w", fixture.Key, recipeKey, err)
			}
		}
		ids[fixture.Key] = row.ID
	}
	return ids, nil
}

func seedClients(tx *gorm.DB, userID uint, workspaceID uint, fixtures []clientFixture) (map[string]uint, error) {
	ids := make(map[string]uint, len(fixtures))
	for _, fixture := range fixtures {
		row := models.Client{
			Name:        fixture.Name,
			Surname:     fixture.Surname,
			Telegram:    fixture.Telegram,
			Instagram:   fixture.Instagram,
			Phone:       fixture.Phone,
			Address:     fixture.Address,
			Source:      fixture.Source,
			UserID:      userID,
			WorkspaceID: &workspaceID,
		}
		if err := tx.Create(&row).Error; err != nil {
			return nil, fmt.Errorf("create client %s: %w", fixture.Key, err)
		}
		ids[fixture.Key] = row.ID
	}
	return ids, nil
}

func seedOrders(tx *gorm.DB, userID uint, workspaceID uint, clientIDs map[string]uint, productIDs map[string]uint, fixtures []orderFixture) error {
	for _, fixture := range fixtures {
		clientID, ok := clientIDs[fixture.ClientKey]
		if !ok {
			return fmt.Errorf("order %s references unknown client %s", fixture.Key, fixture.ClientKey)
		}
		row := models.Order{
			ClientID:    clientID,
			Date:        baseDate.AddDate(0, 0, -fixture.DaysAgo),
			Status:      fixture.Status,
			Comment:     fixture.Comment,
			UserID:      userID,
			WorkspaceID: &workspaceID,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create order %s: %w", fixture.Key, err)
		}
		for _, item := range fixture.Items {
			productID, ok := productIDs[item.ProductKey]
			if !ok {
				return fmt.Errorf("order %s references unknown product %s", fixture.Key, item.ProductKey)
			}
			orderItem := models.OrderItem{
				OrderID:    row.ID,
				ProductID:  productID,
				Quantity:   item.Quantity,
				Price:      item.Price,
				Cost_price: item.CostPrice,
			}
			if err := tx.Create(&orderItem).Error; err != nil {
				return fmt.Errorf("create order item %s/%s: %w", fixture.Key, item.ProductKey, err)
			}
		}
	}
	return nil
}

func seedSessions(tx *gorm.DB, userID uint, workspaceID uint, recipeIDs map[string]uint, ingredientIDs map[string]uint, fixture accountFixture) error {
	recipeByKey := make(map[string]recipeFixture, len(fixture.Recipes))
	for _, recipe := range fixture.Recipes {
		recipeByKey[recipe.Key] = recipe
	}

	for _, session := range fixture.Sessions {
		recipeID, ok := recipeIDs[session.RecipeKey]
		if !ok {
			return fmt.Errorf("session %s references unknown recipe %s", session.Key, session.RecipeKey)
		}
		row := models.CookingSession{
			RecipeID:    recipeID,
			Date:        baseDate.AddDate(0, 0, -session.DaysAgo),
			Yield:       session.Yield,
			UserID:      userID,
			WorkspaceID: &workspaceID,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create cooking session %s: %w", session.Key, err)
		}

		recipe := recipeByKey[session.RecipeKey]
		for _, ingredient := range recipe.Ingredients {
			ingredientID, ok := ingredientIDs[ingredient.IngredientKey]
			if !ok {
				return fmt.Errorf("session %s references unknown ingredient %s", session.Key, ingredient.IngredientKey)
			}
			sessionIngredient := models.CookingSessionIngredient{
				CookingSessionID: row.ID,
				IngredientID:     ingredientID,
				Quantity:         ingredient.Quantity,
				Price:            0,
				Unit:             ingredient.Unit,
			}
			if err := tx.Create(&sessionIngredient).Error; err != nil {
				return fmt.Errorf("create cooking session ingredient %s/%s: %w", session.Key, ingredient.IngredientKey, err)
			}
		}
	}
	return nil
}

func upsertMarker(tx *gorm.DB, username string, workspaceID uint, mode Mode) error {
	marker := DemoSeedMarker{
		Username:       username,
		WorkspaceID:    workspaceID,
		DatasetVersion: DatasetVersion,
		LastMode:       string(mode),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "username"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"workspace_id":    workspaceID,
			"dataset_version": DatasetVersion,
			"last_mode":       string(mode),
			"updated_at":      time.Now(),
		}),
	}).Create(&marker).Error
}

func deleteDemoAccount(tx *gorm.DB, username string) error {
	var user models.User
	err := tx.Unscoped().Where("username = ?", username).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	var personalWorkspaceIDs []uint
	if err := tx.Unscoped().Model(&models.Workspace{}).
		Where("personal_user_id = ?", user.ID).
		Pluck("id", &personalWorkspaceIDs).Error; err != nil {
		return err
	}
	workspaceIDs := append([]uint{}, personalWorkspaceIDs...)
	var marker DemoSeedMarker
	err = tx.Unscoped().Where("username = ?", username).First(&marker).Error
	if err == nil {
		workspaceIDs = append(workspaceIDs, marker.WorkspaceID)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	workspaceIDs = uniqueUint(workspaceIDs)

	for _, workspaceID := range workspaceIDs {
		if err := deleteOperationalData(tx, user.ID, workspaceID); err != nil {
			return err
		}
	}
	if err := tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.WorkspaceMember{}).Error; err != nil {
		return err
	}
	if len(workspaceIDs) > 0 {
		if err := tx.Unscoped().Where("id IN ?", workspaceIDs).Delete(&models.Workspace{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Unscoped().Where("username = ?", username).Delete(&DemoSeedMarker{}).Error; err != nil {
		return err
	}
	return tx.Unscoped().Delete(&user).Error
}

func deleteOperationalData(tx *gorm.DB, userID uint, workspaceID uint) error {
	var orderIDs []uint
	if err := tx.Unscoped().Model(&models.Order{}).
		Where("user_id = ? AND workspace_id = ?", userID, workspaceID).
		Pluck("id", &orderIDs).Error; err != nil {
		return err
	}
	if len(orderIDs) > 0 {
		if err := tx.Unscoped().Where("order_id IN ?", orderIDs).Delete(&models.OrderItem{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Unscoped().Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Delete(&models.Order{}).Error; err != nil {
		return err
	}

	var sessionIDs []uint
	if err := tx.Unscoped().Model(&models.CookingSession{}).
		Where("user_id = ? AND workspace_id = ?", userID, workspaceID).
		Pluck("id", &sessionIDs).Error; err != nil {
		return err
	}
	if len(sessionIDs) > 0 {
		if err := tx.Unscoped().Where("cooking_session_id IN ?", sessionIDs).Delete(&models.CookingSessionIngredient{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Unscoped().Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Delete(&models.CookingSession{}).Error; err != nil {
		return err
	}

	var productIDs []uint
	if err := tx.Unscoped().Model(&models.Product{}).
		Where("user_id = ? AND workspace_id = ?", userID, workspaceID).
		Pluck("id", &productIDs).Error; err != nil {
		return err
	}
	var recipeIDs []uint
	if err := tx.Unscoped().Model(&models.Recipe{}).
		Where("user_id = ? AND workspace_id = ?", userID, workspaceID).
		Pluck("id", &recipeIDs).Error; err != nil {
		return err
	}
	if len(productIDs) > 0 || len(recipeIDs) > 0 {
		query := tx.Unscoped().Where("user_id = ?", userID)
		if len(productIDs) > 0 && len(recipeIDs) > 0 {
			query = query.Where("product_id IN ? OR recipe_id IN ?", productIDs, recipeIDs)
		} else if len(productIDs) > 0 {
			query = query.Where("product_id IN ?", productIDs)
		} else {
			query = query.Where("recipe_id IN ?", recipeIDs)
		}
		if err := query.Delete(&models.ProductOption{}).Error; err != nil {
			return err
		}
	}
	if len(recipeIDs) > 0 {
		if err := tx.Unscoped().Where("recipe_id IN ?", recipeIDs).Delete(&models.RecipeIngredient{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Unscoped().Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Delete(&models.Product{}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Delete(&models.Recipe{}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Delete(&models.Price{}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Delete(&models.Package{}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Delete(&models.Client{}).Error; err != nil {
		return err
	}
	return tx.Unscoped().Where("workspace_id = ?", workspaceID).Delete(&models.WorkspaceIngredient{}).Error
}

func validateUniqueKeys(kind string, username string, keys []string) error {
	seen := map[string]bool{}
	for _, key := range keys {
		if key == "" {
			return fmt.Errorf("%s fixture for %s has empty key", kind, username)
		}
		if seen[key] {
			return fmt.Errorf("%s fixture for %s has duplicate key %s", kind, username, key)
		}
		seen[key] = true
	}
	return nil
}

func ingredientKeys(rows []ingredientFixture) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys
}

func recipeKeys(rows []recipeFixture) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys
}

func packageKeys(rows []packageFixture) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys
}

func productKeys(rows []productFixture) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys
}

func clientKeys(rows []clientFixture) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys
}

func uniqueUint(values []uint) []uint {
	seen := map[uint]bool{}
	unique := make([]uint, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}
