package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"mobile-backend-go/constants"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

type workspaceIngredientFixture struct {
	User              models.User
	PersonalWorkspace models.Workspace
	SecondWorkspace   models.Workspace
	LinkedIngredient  models.Ingredient
	GlobalIngredient  models.Ingredient
	Recipe            models.Recipe
}

func setupWorkspaceIngredientTest(t *testing.T) workspaceIngredientFixture {
	t.Helper()

	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
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
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_workspace_ingredients_test_unique ON workspace_ingredients(workspace_id, ingredient_id) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatalf("create workspace ingredient unique index: %v", err)
	}
	database.DB = db

	user := models.User{Username: "phase3a-test", Password: "hashed"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	personalUserID := user.ID
	personalWorkspace := models.Workspace{
		Name:           "Personal workspace",
		Slug:           "personal-phase3",
		PersonalUserID: &personalUserID,
	}
	secondWorkspace := models.Workspace{
		Name: "Shared workspace",
		Slug: "shared-phase3",
	}
	if err := db.Create(&personalWorkspace).Error; err != nil {
		t.Fatalf("create personal workspace: %v", err)
	}
	if err := db.Create(&secondWorkspace).Error; err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	members := []models.WorkspaceMember{
		{WorkspaceID: personalWorkspace.ID, UserID: user.ID, Role: constants.WorkspaceRoleOwner},
		{WorkspaceID: secondWorkspace.ID, UserID: user.ID, Role: constants.WorkspaceRoleOwner},
	}
	if err := db.Create(&members).Error; err != nil {
		t.Fatalf("create workspace memberships: %v", err)
	}

	linkedIngredient := models.Ingredient{Name: "Workspace salt", Type: "spice"}
	globalIngredient := models.Ingredient{Name: "Global garlic", Type: "spice"}
	if err := db.Create(&linkedIngredient).Error; err != nil {
		t.Fatalf("create linked ingredient: %v", err)
	}
	if err := db.Create(&globalIngredient).Error; err != nil {
		t.Fatalf("create global ingredient: %v", err)
	}

	if _, err := database.EnsureWorkspaceIngredient(db, personalWorkspace.ID, linkedIngredient.ID); err != nil {
		t.Fatalf("link ingredient: %v", err)
	}

	recipe := models.Recipe{Name: "Workspace recipe", UserID: user.ID, WorkspaceID: &personalWorkspace.ID}
	if err := db.Create(&recipe).Error; err != nil {
		t.Fatalf("create recipe: %v", err)
	}

	return workspaceIngredientFixture{
		User:              user,
		PersonalWorkspace: personalWorkspace,
		SecondWorkspace:   secondWorkspace,
		LinkedIngredient:  linkedIngredient,
		GlobalIngredient:  globalIngredient,
		Recipe:            recipe,
	}
}

func TestIngredientsCompatibilityAndWorkspaceScope(t *testing.T) {
	fixture := setupWorkspaceIngredientTest(t)
	createPrice(t, fixture.User.ID, fixture.PersonalWorkspace.ID, fixture.LinkedIngredient.ID, 10)

	globalResponse := runWorkspaceRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		GetIngredients,
		http.MethodGet,
		"/ingredients",
		"/ingredients",
	)
	if globalResponse.Code != http.StatusOK {
		t.Fatalf("global ingredients status = %d body = %s", globalResponse.Code, globalResponse.Body.String())
	}
	var globalIngredients []models.Ingredient
	if err := json.Unmarshal(globalResponse.Body.Bytes(), &globalIngredients); err != nil {
		t.Fatalf("decode global ingredients: %v", err)
	}
	if len(globalIngredients) != 2 {
		t.Fatalf("global ingredient count = %d, want 2", len(globalIngredients))
	}

	workspaceResponse := runWorkspaceRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		GetWorkspaceIngredients,
		http.MethodGet,
		"/workspace-ingredients",
		"/workspace-ingredients",
	)
	if workspaceResponse.Code != http.StatusOK {
		t.Fatalf("workspace ingredients status = %d body = %s", workspaceResponse.Code, workspaceResponse.Body.String())
	}
	var workspaceIngredients []models.WorkspaceIngredient
	if err := json.Unmarshal(workspaceResponse.Body.Bytes(), &workspaceIngredients); err != nil {
		t.Fatalf("decode workspace ingredients: %v", err)
	}
	if len(workspaceIngredients) != 1 {
		t.Fatalf("workspace ingredient count = %d, want 1", len(workspaceIngredients))
	}
	if workspaceIngredients[0].IngredientID != fixture.LinkedIngredient.ID {
		t.Fatalf("workspace ingredient id = %d, want %d", workspaceIngredients[0].IngredientID, fixture.LinkedIngredient.ID)
	}
	if workspaceIngredients[0].LatestPrice == nil || workspaceIngredients[0].LatestPrice.Price != 10 {
		t.Fatalf("workspace latest price = %#v, want 10", workspaceIngredients[0].LatestPrice)
	}
	assertUnitContract(t, workspaceIngredients[0], constants.UnitProfileFineMass, "g", []string{"g", "kg"})
}

func TestSearchIngredientsFindsGlobalNonMember(t *testing.T) {
	fixture := setupWorkspaceIngredientTest(t)

	response := runWorkspaceRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		SearchIngredients,
		http.MethodGet,
		"/ingredients/search",
		"/ingredients/search?query=GARLIC",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d body = %s", response.Code, response.Body.String())
	}
	var ingredients []models.Ingredient
	if err := json.Unmarshal(response.Body.Bytes(), &ingredients); err != nil {
		t.Fatalf("decode search results: %v", err)
	}
	if len(ingredients) != 1 || ingredients[0].ID != fixture.GlobalIngredient.ID {
		t.Fatalf("search result = %#v, want global non-member ingredient", ingredients)
	}
}

func TestCreateIngredientExistingNameEnsuresWorkspaceMembership(t *testing.T) {
	fixture := setupWorkspaceIngredientTest(t)

	response := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		CreateIngredient,
		http.MethodPost,
		"/ingredients",
		"/ingredients",
		map[string]any{
			"name": fixture.GlobalIngredient.Name,
			"type": fixture.GlobalIngredient.Type,
		},
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("create existing status = %d body = %s", response.Code, response.Body.String())
	}
	var conflict map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if uint(conflict["existing_id"].(float64)) != fixture.GlobalIngredient.ID {
		t.Fatalf("existing_id = %v, want %d", conflict["existing_id"], fixture.GlobalIngredient.ID)
	}
	if conflict["workspace_linked"] != true {
		t.Fatalf("workspace_linked = %v, want true", conflict["workspace_linked"])
	}
	assertWorkspaceIngredientExists(t, fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID)
}

func TestPriceAndRecipeWritesAutoLinkGlobalIngredientsWhenStrictModeDisabled(t *testing.T) {
	t.Setenv("STRICT_WORKSPACE_INGREDIENTS", "false")
	fixture := setupWorkspaceIngredientTest(t)

	priceResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddPrice,
		http.MethodPost,
		"/prices",
		"/prices",
		models.PriceCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
			Price:        12,
			Quantity:     1,
			Unit:         "kg",
			Date:         time.Now(),
		},
	)
	if priceResponse.Code != http.StatusCreated {
		t.Fatalf("add price status = %d body = %s", priceResponse.Code, priceResponse.Body.String())
	}
	assertWorkspaceIngredientExists(t, fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID)

	otherIngredient := models.Ingredient{Name: "Global paprika", Type: "spice"}
	if err := database.DB.Create(&otherIngredient).Error; err != nil {
		t.Fatalf("create second global ingredient: %v", err)
	}
	recipeIngredientResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddIngredientToRecipe,
		http.MethodPost,
		"/recipes/:id/ingredients",
		"/recipes/"+uintToString(fixture.Recipe.ID)+"/ingredients",
		models.RecipeIngredientCreateDTO{
			IngredientID: otherIngredient.ID,
			Quantity:     "10",
			Unit:         "g",
		},
	)
	if recipeIngredientResponse.Code != http.StatusCreated {
		t.Fatalf("add recipe ingredient status = %d body = %s", recipeIngredientResponse.Code, recipeIngredientResponse.Body.String())
	}
	assertWorkspaceIngredientExists(t, fixture.PersonalWorkspace.ID, otherIngredient.ID)
}

func TestPriceAndRecipeWritesRejectUnlinkedIngredientsWhenStrictModeEnabled(t *testing.T) {
	t.Setenv("STRICT_WORKSPACE_INGREDIENTS", "true")
	fixture := setupWorkspaceIngredientTest(t)

	priceResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddPrice,
		http.MethodPost,
		"/prices",
		"/prices",
		models.PriceCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
			Price:        12,
			Quantity:     1,
			Unit:         "kg",
			Date:         time.Now(),
		},
	)
	if priceResponse.Code != http.StatusBadRequest {
		t.Fatalf("strict unlinked price status = %d body = %s", priceResponse.Code, priceResponse.Body.String())
	}
	assertJSONError(t, priceResponse, "Ingredient is not in workspace")
	assertWorkspaceIngredientCount(t, fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID, 0)
	assertPriceCount(t, fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID, 0)

	recipeIngredientResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddIngredientToRecipe,
		http.MethodPost,
		"/recipes/:id/ingredients",
		"/recipes/"+uintToString(fixture.Recipe.ID)+"/ingredients",
		models.RecipeIngredientCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
			Quantity:     "10",
			Unit:         "g",
		},
	)
	if recipeIngredientResponse.Code != http.StatusBadRequest {
		t.Fatalf("strict unlinked recipe ingredient status = %d body = %s", recipeIngredientResponse.Code, recipeIngredientResponse.Body.String())
	}
	assertJSONError(t, recipeIngredientResponse, "Ingredient is not in workspace")
	assertRecipeIngredientCount(t, fixture.Recipe.ID, fixture.GlobalIngredient.ID, 0)
}

func TestPriceAndRecipeWritesAcceptExplicitlyLinkedIngredientsWhenStrictModeEnabled(t *testing.T) {
	t.Setenv("STRICT_WORKSPACE_INGREDIENTS", "true")
	fixture := setupWorkspaceIngredientTest(t)

	linkResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddWorkspaceIngredient,
		http.MethodPost,
		"/workspace-ingredients",
		"/workspace-ingredients",
		models.WorkspaceIngredientCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
		},
	)
	if linkResponse.Code != http.StatusCreated {
		t.Fatalf("link workspace ingredient status = %d body = %s", linkResponse.Code, linkResponse.Body.String())
	}

	priceResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddPrice,
		http.MethodPost,
		"/prices",
		"/prices",
		models.PriceCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
			Price:        12,
			Quantity:     1,
			Unit:         "kg",
			Date:         time.Now(),
		},
	)
	if priceResponse.Code != http.StatusCreated {
		t.Fatalf("strict linked price status = %d body = %s", priceResponse.Code, priceResponse.Body.String())
	}
	assertPriceCount(t, fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID, 1)

	recipeIngredientResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddIngredientToRecipe,
		http.MethodPost,
		"/recipes/:id/ingredients",
		"/recipes/"+uintToString(fixture.Recipe.ID)+"/ingredients",
		models.RecipeIngredientCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
			Quantity:     "10",
			Unit:         "g",
		},
	)
	if recipeIngredientResponse.Code != http.StatusCreated {
		t.Fatalf("strict linked recipe ingredient status = %d body = %s", recipeIngredientResponse.Code, recipeIngredientResponse.Body.String())
	}
	assertRecipeIngredientCount(t, fixture.Recipe.ID, fixture.GlobalIngredient.ID, 1)
}

func TestPriceAndRecipeWritesRejectInactiveIngredientsWhenStrictModeEnabled(t *testing.T) {
	t.Setenv("STRICT_WORKSPACE_INGREDIENTS", "true")
	fixture := setupWorkspaceIngredientTest(t)

	if err := database.DB.Model(&models.WorkspaceIngredient{}).
		Where("workspace_id = ? AND ingredient_id = ?", fixture.PersonalWorkspace.ID, fixture.LinkedIngredient.ID).
		Update("active", false).Error; err != nil {
		t.Fatalf("deactivate workspace ingredient: %v", err)
	}

	priceResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddPrice,
		http.MethodPost,
		"/prices",
		"/prices",
		models.PriceCreateDTO{
			IngredientID: fixture.LinkedIngredient.ID,
			Price:        12,
			Quantity:     1,
			Unit:         "kg",
			Date:         time.Now(),
		},
	)
	if priceResponse.Code != http.StatusBadRequest {
		t.Fatalf("strict inactive price status = %d body = %s", priceResponse.Code, priceResponse.Body.String())
	}
	assertJSONError(t, priceResponse, "Ingredient is not in workspace")
	assertPriceCount(t, fixture.PersonalWorkspace.ID, fixture.LinkedIngredient.ID, 0)

	recipeIngredientResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddIngredientToRecipe,
		http.MethodPost,
		"/recipes/:id/ingredients",
		"/recipes/"+uintToString(fixture.Recipe.ID)+"/ingredients",
		models.RecipeIngredientCreateDTO{
			IngredientID: fixture.LinkedIngredient.ID,
			Quantity:     "10",
			Unit:         "g",
		},
	)
	if recipeIngredientResponse.Code != http.StatusBadRequest {
		t.Fatalf("strict inactive recipe ingredient status = %d body = %s", recipeIngredientResponse.Code, recipeIngredientResponse.Body.String())
	}
	assertJSONError(t, recipeIngredientResponse, "Ingredient is not in workspace")
	assertRecipeIngredientCount(t, fixture.Recipe.ID, fixture.LinkedIngredient.ID, 0)
}

func TestWorkspaceIngredientEndpointsManageMembership(t *testing.T) {
	fixture := setupWorkspaceIngredientTest(t)

	postResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddWorkspaceIngredient,
		http.MethodPost,
		"/workspace-ingredients",
		"/workspace-ingredients",
		models.WorkspaceIngredientCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
		},
	)
	if postResponse.Code != http.StatusCreated {
		t.Fatalf("link workspace ingredient status = %d body = %s", postResponse.Code, postResponse.Body.String())
	}
	assertWorkspaceIngredientExists(t, fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID)

	secondPostResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddWorkspaceIngredient,
		http.MethodPost,
		"/workspace-ingredients",
		"/workspace-ingredients",
		models.WorkspaceIngredientCreateDTO{
			IngredientID: fixture.GlobalIngredient.ID,
		},
	)
	if secondPostResponse.Code != http.StatusCreated {
		t.Fatalf("idempotent link status = %d body = %s", secondPostResponse.Code, secondPostResponse.Body.String())
	}
	assertWorkspaceIngredientCount(t, fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID, 1)

	var linked models.WorkspaceIngredient
	if err := database.DB.
		Where("workspace_id = ? AND ingredient_id = ?", fixture.PersonalWorkspace.ID, fixture.GlobalIngredient.ID).
		First(&linked).Error; err != nil {
		t.Fatalf("load linked workspace ingredient: %v", err)
	}

	alias := "House garlic"
	category := "Seasoning"
	patchResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		UpdateWorkspaceIngredient,
		http.MethodPatch,
		"/workspace-ingredients/:id",
		"/workspace-ingredients/"+uintToString(linked.ID),
		models.WorkspaceIngredientUpdateDTO{
			Alias:    &alias,
			Category: &category,
		},
	)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("patch workspace ingredient status = %d body = %s", patchResponse.Code, patchResponse.Body.String())
	}
	var patched models.WorkspaceIngredient
	if err := json.Unmarshal(patchResponse.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patched workspace ingredient: %v", err)
	}
	if patched.Alias != alias || patched.Category != category || !patched.Active {
		t.Fatalf("patched workspace ingredient = %#v, want alias/category and active true", patched)
	}
	assertUnitContract(t, patched, constants.UnitProfileFineMass, "g", []string{"g", "kg"})

	active := false
	partialPatchResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		UpdateWorkspaceIngredient,
		http.MethodPatch,
		"/workspace-ingredients/:id",
		"/workspace-ingredients/"+uintToString(linked.ID),
		models.WorkspaceIngredientUpdateDTO{
			Active: &active,
		},
	)
	if partialPatchResponse.Code != http.StatusOK {
		t.Fatalf("partial patch workspace ingredient status = %d body = %s", partialPatchResponse.Code, partialPatchResponse.Body.String())
	}
	var partialPatched models.WorkspaceIngredient
	if err := json.Unmarshal(partialPatchResponse.Body.Bytes(), &partialPatched); err != nil {
		t.Fatalf("decode partial patched workspace ingredient: %v", err)
	}
	if partialPatched.Alias != alias || partialPatched.Category != category || partialPatched.Active {
		t.Fatalf("partial patched workspace ingredient = %#v, want preserved metadata and inactive", partialPatched)
	}

	deleteResponse := runWorkspaceRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		DeleteWorkspaceIngredient,
		http.MethodDelete,
		"/workspace-ingredients/:id",
		"/workspace-ingredients/"+uintToString(linked.ID),
	)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete workspace ingredient status = %d body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	var afterDelete models.WorkspaceIngredient
	if err := database.DB.
		Where("id = ?", linked.ID).
		First(&afterDelete).Error; err != nil {
		t.Fatalf("load workspace ingredient after delete: %v", err)
	}
	if afterDelete.Active {
		t.Fatalf("workspace ingredient active = true, want false")
	}
	var globalIngredient models.Ingredient
	if err := database.DB.First(&globalIngredient, fixture.GlobalIngredient.ID).Error; err != nil {
		t.Fatalf("global ingredient was deleted: %v", err)
	}
}

func TestEnsureWorkspaceIngredientIsConcurrentSafe(t *testing.T) {
	fixture := setupWorkspaceIngredientTest(t)

	const workers = 8
	errCh := make(chan error, workers)
	for range workers {
		go func() {
			_, err := database.EnsureWorkspaceIngredient(
				database.DB,
				fixture.SecondWorkspace.ID,
				fixture.GlobalIngredient.ID,
			)
			errCh <- err
		}()
	}

	for range workers {
		if err := <-errCh; err != nil {
			t.Fatalf("ensure workspace ingredient concurrently: %v", err)
		}
	}
	assertWorkspaceIngredientCount(t, fixture.SecondWorkspace.ID, fixture.GlobalIngredient.ID, 1)
}

func TestPriceAndRecipeWritesRejectInvalidIngredientIDs(t *testing.T) {
	fixture := setupWorkspaceIngredientTest(t)
	invalidID := fixture.GlobalIngredient.ID + 1000

	priceResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddPrice,
		http.MethodPost,
		"/prices",
		"/prices",
		models.PriceCreateDTO{
			IngredientID: invalidID,
			Price:        12,
			Quantity:     1,
			Unit:         "kg",
			Date:         time.Now(),
		},
	)
	if priceResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid price ingredient status = %d body = %s", priceResponse.Code, priceResponse.Body.String())
	}

	recipeIngredientResponse := runWorkspaceJSONRequest(
		fixture.User.ID,
		fixture.PersonalWorkspace.ID,
		AddIngredientToRecipe,
		http.MethodPost,
		"/recipes/:id/ingredients",
		"/recipes/"+uintToString(fixture.Recipe.ID)+"/ingredients",
		models.RecipeIngredientCreateDTO{
			IngredientID: invalidID,
			Quantity:     "10",
			Unit:         "g",
		},
	)
	if recipeIngredientResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid recipe ingredient status = %d body = %s", recipeIngredientResponse.Code, recipeIngredientResponse.Body.String())
	}
}

func assertWorkspaceIngredientExists(t *testing.T, workspaceID uint, ingredientID uint) {
	t.Helper()

	assertWorkspaceIngredientCount(t, workspaceID, ingredientID, 1)
}

func assertWorkspaceIngredientCount(t *testing.T, workspaceID uint, ingredientID uint, want int64) {
	t.Helper()

	var count int64
	if err := database.DB.Model(&models.WorkspaceIngredient{}).
		Where("workspace_id = ? AND ingredient_id = ? AND active = ?", workspaceID, ingredientID, true).
		Count(&count).Error; err != nil {
		t.Fatalf("count workspace ingredients: %v", err)
	}
	if count != want {
		t.Fatalf("workspace ingredient count for workspace=%d ingredient=%d is %d, want %d", workspaceID, ingredientID, count, want)
	}
}

func assertPriceCount(t *testing.T, workspaceID uint, ingredientID uint, want int64) {
	t.Helper()

	var count int64
	if err := database.DB.Model(&models.Price{}).
		Where("workspace_id = ? AND ingredient_id = ?", workspaceID, ingredientID).
		Count(&count).Error; err != nil {
		t.Fatalf("count prices: %v", err)
	}
	if count != want {
		t.Fatalf("price count for workspace=%d ingredient=%d is %d, want %d", workspaceID, ingredientID, count, want)
	}
}

func assertRecipeIngredientCount(t *testing.T, recipeID uint, ingredientID uint, want int64) {
	t.Helper()

	var count int64
	if err := database.DB.Model(&models.RecipeIngredient{}).
		Where("recipe_id = ? AND ingredient_id = ?", recipeID, ingredientID).
		Count(&count).Error; err != nil {
		t.Fatalf("count recipe ingredients: %v", err)
	}
	if count != want {
		t.Fatalf("recipe ingredient count for recipe=%d ingredient=%d is %d, want %d", recipeID, ingredientID, count, want)
	}
}

func assertUnitContract(t *testing.T, workspaceIngredient models.WorkspaceIngredient, wantProfile string, wantDefault string, wantAllowed []string) {
	t.Helper()

	if workspaceIngredient.UnitProfile != wantProfile {
		t.Fatalf("unit profile = %q, want %q", workspaceIngredient.UnitProfile, wantProfile)
	}
	if workspaceIngredient.DefaultUnit != wantDefault {
		t.Fatalf("default unit = %q, want %q", workspaceIngredient.DefaultUnit, wantDefault)
	}
	if len(workspaceIngredient.AllowedUnits) != len(wantAllowed) {
		t.Fatalf("allowed units = %#v, want %#v", workspaceIngredient.AllowedUnits, wantAllowed)
	}
	for index := range wantAllowed {
		if workspaceIngredient.AllowedUnits[index] != wantAllowed[index] {
			t.Fatalf("allowed units = %#v, want %#v", workspaceIngredient.AllowedUnits, wantAllowed)
		}
	}
}

func assertJSONError(t *testing.T, response *httptest.ResponseRecorder, want string) {
	t.Helper()

	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload["error"] != want {
		t.Fatalf("error = %q, want %q", payload["error"], want)
	}
}
