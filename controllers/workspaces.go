package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"mobile-backend-go/constants"
	"mobile-backend-go/currencycatalog"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// WorkspaceResponse represents a workspace available to the authenticated user.
type WorkspaceResponse struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Currency  string `json:"currency"`
	AccountID *uint  `json:"account_id,omitempty"`
	Role      string `json:"role"`
}

// GetCurrencies returns the supported ISO 4217 monetary currencies.
// @Summary Get supported currencies
// @Tags Workspaces
// @Security BearerAuth
// @Produce json
// @Success 200 {array} currencycatalog.Currency
// @Failure 401 {object} map[string]string "Unauthorized"
// @Router /api/currencies [get]
func GetCurrencies(c *gin.Context) {
	c.JSON(http.StatusOK, currencycatalog.List())
}

// GetWorkspaces returns workspaces available to the authenticated user.
// @Summary Get accessible workspaces
// @Description Get workspaces available to the authenticated user. This bootstrap endpoint only requires JWT authentication and ignores X-Workspace-ID.
// @Tags Workspaces
// @Security BearerAuth
// @Produce json
// @Success 200 {array} WorkspaceResponse
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/workspaces [get]
func GetWorkspaces(c *gin.Context) {
	userID := c.MustGet("userID").(uint)

	var memberships []models.WorkspaceMember
	if err := database.DB.
		Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id AND workspaces.deleted_at IS NULL").
		Preload("Workspace").
		Where("user_id = ?", userID).
		Find(&memberships).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch workspaces"})
		return
	}

	response := make([]WorkspaceResponse, 0, len(memberships))
	for _, membership := range memberships {
		response = append(response, workspaceResponseFromMembership(membership))
	}

	c.JSON(http.StatusOK, response)
}

// GetCurrentWorkspace returns the workspace resolved for the current request.
// @Summary Get current workspace
// @Description Get the workspace resolved for the current request. Uses X-Workspace-ID when present, otherwise falls back to the user's personal workspace.
// @Tags Workspaces
// @Security BearerAuth
// @Produce json
// @Param X-Workspace-ID header int false "Workspace ID"
// @Success 200 {object} WorkspaceResponse
// @Failure 400 {object} map[string]string "Invalid workspace ID"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Workspace access denied"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/workspaces/current [get]
func GetCurrentWorkspace(c *gin.Context) {
	workspaceValue, exists := c.Get("workspace")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Workspace context missing"})
		return
	}
	workspace, ok := workspaceValue.(models.Workspace)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid workspace context"})
		return
	}

	roleValue, exists := c.Get("workspaceRole")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Workspace role missing"})
		return
	}
	role, ok := roleValue.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid workspace role"})
		return
	}

	c.JSON(http.StatusOK, WorkspaceResponse{
		ID:        workspace.ID,
		Name:      workspace.Name,
		Slug:      workspace.Slug,
		Currency:  workspace.Currency,
		AccountID: workspace.AccountID,
		Role:      role,
	})
}

// UpdateCurrentWorkspaceCurrency updates the current workspace currency for an owner.
// @Summary Update current workspace currency
// @Tags Workspaces
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param X-Workspace-ID header int false "Workspace ID"
// @Param request body object true "Currency update"
// @Success 200 {object} WorkspaceResponse
// @Failure 400 {object} map[string]string "Invalid currency"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Workspace owner access required"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /api/workspaces/current [patch]
func UpdateCurrentWorkspaceCurrency(c *gin.Context) {
	userID := c.MustGet("userID").(uint)
	workspaceID := c.MustGet("workspaceID").(uint)

	currency, err := decodeCurrencyUpdate(c)
	if err != nil || !currencycatalog.IsAllowed(currency) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Currency must be a supported uppercase ISO 4217 code"})
		return
	}

	result := database.DB.Model(&models.Workspace{}).
		Where(`id = ? AND EXISTS (
			SELECT 1 FROM workspace_members
			WHERE workspace_members.workspace_id = workspaces.id
			AND workspace_members.user_id = ?
			AND workspace_members.role = ?
			AND workspace_members.deleted_at IS NULL
		)`, workspaceID, userID, constants.WorkspaceRoleOwner).
		Update("currency", currency)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update workspace currency"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Workspace owner access required"})
		return
	}

	member, found, err := database.FindWorkspaceMember(database.DB, userID, workspaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch workspace"})
		return
	}
	if !found {
		c.JSON(http.StatusForbidden, gin.H{"error": "Workspace access denied"})
		return
	}

	c.JSON(http.StatusOK, workspaceResponseFromMembership(member))
}

func decodeCurrencyUpdate(c *gin.Context) (string, error) {
	decoder := json.NewDecoder(c.Request.Body)
	var payload map[string]json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return "", err
	}
	if len(payload) != 1 {
		return "", errors.New("request must contain exactly one field")
	}
	rawCurrency, ok := payload["currency"]
	if !ok {
		return "", errors.New("currency field is required")
	}
	var currency string
	if err := json.Unmarshal(rawCurrency, &currency); err != nil {
		return "", err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("request must contain one JSON object")
		}
		return "", err
	}
	return currency, nil
}

func workspaceResponseFromMembership(membership models.WorkspaceMember) WorkspaceResponse {
	return WorkspaceResponse{
		ID:        membership.Workspace.ID,
		Name:      membership.Workspace.Name,
		Slug:      membership.Workspace.Slug,
		Currency:  membership.Workspace.Currency,
		AccountID: membership.Workspace.AccountID,
		Role:      membership.Role,
	}
}
