package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"mobile-backend-go/constants"
	"mobile-backend-go/database"
	"mobile-backend-go/middleware"
	"mobile-backend-go/models"
)

type workspaceControllerFixture struct {
	Owner          models.User
	NonOwner       models.User
	Outsider       models.User
	Workspace      models.Workspace
	OtherWorkspace models.Workspace
}

func setupWorkspaceControllerTest(t *testing.T) workspaceControllerFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

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
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	database.DB = db

	owner := models.User{Username: "workspace-owner", Password: "hashed"}
	nonOwner := models.User{Username: "workspace-manager", Password: "hashed"}
	outsider := models.User{Username: "workspace-outsider", Password: "hashed"}
	if err := db.Create(&[]models.User{owner, nonOwner, outsider}).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Where("username = ?", owner.Username).First(&owner).Error; err != nil {
		t.Fatalf("reload owner: %v", err)
	}
	if err := db.Where("username = ?", nonOwner.Username).First(&nonOwner).Error; err != nil {
		t.Fatalf("reload non-owner: %v", err)
	}
	if err := db.Where("username = ?", outsider.Username).First(&outsider).Error; err != nil {
		t.Fatalf("reload outsider: %v", err)
	}

	workspace := models.Workspace{Name: "Shared kitchen", Slug: "shared-kitchen", Currency: "RSD"}
	otherWorkspace := models.Workspace{Name: "Other kitchen", Slug: "other-kitchen", Currency: "USD"}
	if err := db.Create(&[]models.Workspace{workspace, otherWorkspace}).Error; err != nil {
		t.Fatalf("create workspaces: %v", err)
	}
	if err := db.Where("slug = ?", workspace.Slug).First(&workspace).Error; err != nil {
		t.Fatalf("reload workspace: %v", err)
	}
	if err := db.Where("slug = ?", otherWorkspace.Slug).First(&otherWorkspace).Error; err != nil {
		t.Fatalf("reload other workspace: %v", err)
	}

	memberships := []models.WorkspaceMember{
		{WorkspaceID: workspace.ID, UserID: owner.ID, Role: constants.WorkspaceRoleOwner},
		{WorkspaceID: workspace.ID, UserID: nonOwner.ID, Role: constants.WorkspaceRoleManager},
		{WorkspaceID: otherWorkspace.ID, UserID: owner.ID, Role: constants.WorkspaceRoleOwner},
	}
	if err := db.Create(&memberships).Error; err != nil {
		t.Fatalf("create memberships: %v", err)
	}

	return workspaceControllerFixture{owner, nonOwner, outsider, workspace, otherWorkspace}
}

func runWorkspaceControllerRequest(userID, workspaceID uint, handler gin.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	router := gin.New()
	router.Handle(method, path, func(c *gin.Context) {
		c.Set("userID", userID)
		c.Set("workspaceID", workspaceID)
		handler(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestWorkspaceResponsesIncludeCurrency(t *testing.T) {
	fixture := setupWorkspaceControllerTest(t)

	listResponse := runWorkspaceControllerRequest(fixture.Owner.ID, 0, GetWorkspaces, http.MethodGet, "/workspaces", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", listResponse.Code, listResponse.Body.String())
	}
	var workspaces []WorkspaceResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &workspaces); err != nil {
		t.Fatalf("decode workspace list: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("workspace list length = %d, want 2", len(workspaces))
	}
	for _, workspace := range workspaces {
		if workspace.Currency == "" {
			t.Fatalf("workspace response omitted currency: %+v", workspace)
		}
	}

	currentRouter := gin.New()
	currentRouter.Use(func(c *gin.Context) { c.Set("userID", fixture.Owner.ID) })
	currentRouter.Use(middleware.WorkspaceMiddleware())
	currentRouter.GET("/workspaces/current", GetCurrentWorkspace)
	currentResponse := httptest.NewRecorder()
	currentRequest := httptest.NewRequest(http.MethodGet, "/workspaces/current", nil)
	currentRequest.Header.Set("X-Workspace-ID", strconv.FormatUint(uint64(fixture.Workspace.ID), 10))
	currentRouter.ServeHTTP(currentResponse, currentRequest)
	if currentResponse.Code != http.StatusOK {
		t.Fatalf("current status = %d body = %s", currentResponse.Code, currentResponse.Body.String())
	}
	var current WorkspaceResponse
	if err := json.Unmarshal(currentResponse.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode current workspace: %v", err)
	}
	if current.Currency != "RSD" {
		t.Fatalf("current workspace currency = %q, want RSD", current.Currency)
	}
}

func TestUpdateCurrentWorkspaceCurrencyRequiresOwnerAndValidCurrency(t *testing.T) {
	fixture := setupWorkspaceControllerTest(t)

	for _, body := range []string{"{}", `{"currency":"eur"}`, `{"currency":"EUR","extra":true}`, `{"currency":"BGN"}`} {
		response := runWorkspaceControllerRequest(fixture.Owner.ID, fixture.Workspace.ID, UpdateCurrentWorkspaceCurrency, http.MethodPatch, "/workspaces/current", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d body = %s, want 400", body, response.Code, response.Body.String())
		}
	}

	nonOwnerResponse := runWorkspaceControllerRequest(fixture.NonOwner.ID, fixture.Workspace.ID, UpdateCurrentWorkspaceCurrency, http.MethodPatch, "/workspaces/current", `{"currency":"EUR"}`)
	if nonOwnerResponse.Code != http.StatusForbidden {
		t.Fatalf("non-owner status = %d body = %s, want 403", nonOwnerResponse.Code, nonOwnerResponse.Body.String())
	}
	var unchanged models.Workspace
	if err := database.DB.First(&unchanged, fixture.Workspace.ID).Error; err != nil {
		t.Fatalf("reload unchanged workspace: %v", err)
	}
	if unchanged.Currency != "RSD" {
		t.Fatalf("non-owner changed currency to %q", unchanged.Currency)
	}

	response := runWorkspaceControllerRequest(fixture.Owner.ID, fixture.Workspace.ID, UpdateCurrentWorkspaceCurrency, http.MethodPatch, "/workspaces/current", `{"currency":"EUR"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("owner status = %d body = %s", response.Code, response.Body.String())
	}
	var updated WorkspaceResponse
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated workspace: %v", err)
	}
	if updated.Currency != "EUR" || updated.ID != fixture.Workspace.ID {
		t.Fatalf("updated workspace = %+v, want EUR for %d", updated, fixture.Workspace.ID)
	}
}

func TestWorkspaceMiddlewareRejectsInaccessibleCurrencyUpdate(t *testing.T) {
	fixture := setupWorkspaceControllerTest(t)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", fixture.Outsider.ID) })
	router.Use(middleware.WorkspaceMiddleware())
	router.PATCH("/workspaces/current", UpdateCurrentWorkspaceCurrency)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/workspaces/current", bytes.NewBufferString(`{"currency":"EUR"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Workspace-ID", strconv.FormatUint(uint64(fixture.Workspace.ID), 10))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("inaccessible workspace status = %d body = %s, want 403", recorder.Code, recorder.Body.String())
	}
}

func TestGetCurrenciesReturnsCatalog(t *testing.T) {
	router := gin.New()
	router.GET("/currencies", GetCurrencies)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/currencies", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var currencies []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &currencies); err != nil {
		t.Fatalf("decode currencies: %v", err)
	}
	if len(currencies) != 155 || currencies[0].Code == "" {
		t.Fatalf("catalog = %+v, want 155 currencies with codes", currencies)
	}
}
