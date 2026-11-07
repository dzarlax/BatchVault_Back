package controllers

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func CookingEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("COOKING_ENABLED")), "true")
}
func cookingService(c *gin.Context) services.CookingService {
	return services.CookingService{DB: database.DB, WorkspaceID: c.MustGet("workspaceID").(uint), UserID: c.MustGet("userID").(uint)}
}
func cookingWriteAllowed(c *gin.Context) bool {
	role, _ := c.Get("workspaceRole")
	if role == "viewer" {
		c.JSON(403, gin.H{"code": "read_only", "error": "This workspace role is read-only"})
		return false
	}
	return true
}
func requireNewCooking(c *gin.Context) bool {
	if !CookingEnabled() {
		c.JSON(503, gin.H{"code": "cooking_disabled", "error": "Cooking is not enabled on this server"})
		return false
	}
	return true
}
func cookingFailure(c *gin.Context, err error) {
	var conflict *services.CookingConflict
	var invalid *services.CookingCalculationError
	switch {
	case errors.Is(err, errCookingReadOnly):
		c.JSON(403, gin.H{"code": "read_only", "error": "This role cannot change inventory-tracked orders"})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(404, gin.H{"code": "not_found", "error": "Cooking resource was not found"})
	case errors.As(err, &conflict):
		c.JSON(409, gin.H{"code": conflict.Code, "error": conflict.Message})
	case errors.As(err, &invalid):
		c.JSON(400, gin.H{"code": invalid.Code, "field": invalid.Field, "error": invalid.Message})
	default:
		c.JSON(500, gin.H{"code": "cooking_failed", "error": "Cooking operation failed"})
	}
}
func cookingID(c *gin.Context) (uint, bool) {
	id, e := strconv.ParseUint(c.Param("id"), 10, 32)
	if e != nil || id == 0 {
		c.JSON(400, gin.H{"code": "invalid_id", "error": "A positive cooking ID is required"})
		return 0, false
	}
	return uint(id), true
}
func cookingBind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	if e := c.ShouldBindJSON(v); e != nil {
		c.JSON(400, gin.H{"code": "invalid_input", "error": "Invalid cooking request"})
		return false
	}
	return true
}
func cookingResult(c *gin.Context, result json.RawMessage, e error) {
	if e != nil {
		cookingFailure(c, e)
		return
	}
	c.Data(200, "application/json", result)
}

// PreviewCookingPlan handles the workspace-scoped cooking contract.
// @Summary Preview order demand and selected finished stock
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingPlanInput true "Cooking request; quantities are decimal strings"
// @Success 200 {object} models.CookingPlanPreview
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking/plan-preview [post]
func PreviewCookingPlan(c *gin.Context) {
	if !requireNewCooking(c) {
		return
	}
	var input models.CookingPlanInput
	if !cookingBind(c, &input) {
		return
	}
	out, e := cookingService(c).Plan(input)
	if e != nil {
		cookingFailure(c, e)
		return
	}
	c.JSON(200, out)
}

// CreateCookingRun handles the workspace-scoped cooking contract.
// @Summary Create a cooking plan and reserve selected stock
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingCreateInput true "Cooking request; quantities are decimal strings"
// @Success 200 {object} models.CookingRun
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs [post]
func CreateCookingRun(c *gin.Context) {
	if !requireNewCooking(c) || !cookingWriteAllowed(c) {
		return
	}
	var input models.CookingCreateInput
	if !cookingBind(c, &input) {
		return
	}
	v, e := cookingService(c).Create(input)
	cookingResult(c, v, e)
}

// ListCookingRuns handles the workspace-scoped cooking contract.
// @Summary List cooking plans by descending cursor
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Param cursor query string false "Previous next_cursor; at most 50 results"
// @Success 200 {object} map[string]interface{}
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs [get]
func ListCookingRuns(c *gin.Context) {
	cursor := uint64(0)
	var e error
	if c.Query("cursor") != "" {
		cursor, e = strconv.ParseUint(c.Query("cursor"), 10, 32)
		if e != nil {
			c.JSON(400, gin.H{"code": "invalid_cursor", "error": "Invalid list cursor"})
			return
		}
	}
	items, next, e := cookingService(c).List(uint(cursor))
	if e != nil {
		cookingFailure(c, e)
		return
	}
	var nextCursor *string
	if next != nil {
		n := strconv.FormatUint(uint64(*next), 10)
		nextCursor = &n
	}
	c.JSON(200, gin.H{"items": items, "next_cursor": nextCursor})
}

// GetCookingRun handles the workspace-scoped cooking contract.
// @Summary Read a cooking run with snapshots and history
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Param id path int true "Cooking run ID"
// @Success 200 {object} models.CookingRun
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs/{id} [get]
func GetCookingRun(c *gin.Context) {
	id, ok := cookingID(c)
	if !ok {
		return
	}
	v, e := cookingService(c).Run(id)
	if e != nil {
		cookingFailure(c, e)
		return
	}
	c.JSON(200, v)
}

// PreviewCookingPreparation handles the workspace-scoped cooking contract.
// @Summary Calculate marinades from actual main ingredient weights
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingPreparationInput true "Cooking request; quantities are decimal strings"
// @Success 200 {object} models.CookingPreparation
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking/preparation-preview [post]
func PreviewCookingPreparation(c *gin.Context) {
	var input models.CookingPreparationInput
	if !cookingBind(c, &input) {
		return
	}
	if input.RunID == nil && !requireNewCooking(c) {
		return
	}
	v, e := cookingService(c).PreparationPreview(input)
	if e != nil {
		cookingFailure(c, e)
		return
	}
	c.JSON(200, v)
}

// SaveCookingPreparation handles the workspace-scoped cooking contract.
// @Summary Save actual main ingredient distribution and frozen recipes
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingPreparationInput true "Cooking request; quantities are decimal strings"
// @Param id path int true "Cooking run ID"
// @Success 200 {object} models.CookingRun
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs/{id}/preparation [put]
func SaveCookingPreparation(c *gin.Context) {
	if !cookingWriteAllowed(c) {
		return
	}
	id, ok := cookingID(c)
	if !ok {
		return
	}
	var input models.CookingPreparationInput
	if !cookingBind(c, &input) {
		return
	}
	v, e := cookingService(c).Prepare(id, input)
	cookingResult(c, v, e)
}

// PreviewCookingPacking handles the workspace-scoped cooking contract.
// @Summary Preview packaging consumption without writing stock
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingPackingInput true "Cooking request; quantities are decimal strings"
// @Param id path int true "Cooking run ID"
// @Success 200 {object} models.CookingPackingPreview
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs/{id}/packing-preview [post]
func PreviewCookingPacking(c *gin.Context) {
	id, ok := cookingID(c)
	if !ok {
		return
	}
	var input models.CookingPackingInput
	if !cookingBind(c, &input) {
		return
	}
	v, e := cookingService(c).PackingPreview(id, input)
	if e != nil {
		cookingFailure(c, e)
		return
	}
	c.JSON(200, v)
}

// SaveCookingResult handles the workspace-scoped cooking contract.
// @Summary Record usable output and optional first packing atomically
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingResultInput true "Cooking request; quantities are decimal strings"
// @Param id path int true "Cooking run ID"
// @Success 200 {object} models.CookingRun
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs/{id}/results [post]
func SaveCookingResult(c *gin.Context) {
	if !cookingWriteAllowed(c) {
		return
	}
	id, ok := cookingID(c)
	if !ok {
		return
	}
	var input models.CookingResultInput
	if !cookingBind(c, &input) {
		return
	}
	v, e := cookingService(c).Results(id, input)
	cookingResult(c, v, e)
}

// SaveCookingPacking handles the workspace-scoped cooking contract.
// @Summary Pack more finished stock using frozen compositions
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingPackingInput true "Cooking request; quantities are decimal strings"
// @Param id path int true "Cooking run ID"
// @Success 200 {object} models.CookingRun
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs/{id}/packings [post]
func SaveCookingPacking(c *gin.Context) {
	if !cookingWriteAllowed(c) {
		return
	}
	id, ok := cookingID(c)
	if !ok {
		return
	}
	var input models.CookingPackingInput
	if !cookingBind(c, &input) {
		return
	}
	v, e := cookingService(c).Pack(id, input)
	cookingResult(c, v, e)
}

// ArchiveCookingRun handles the workspace-scoped cooking contract.
// @Summary Archive a run and release its remaining reservations
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingArchiveInput true "Cooking request; quantities are decimal strings"
// @Param id path int true "Cooking run ID"
// @Success 200 {object} models.CookingRun
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs/{id}/archive [post]
func ArchiveCookingRun(c *gin.Context) {
	if !cookingWriteAllowed(c) {
		return
	}
	id, ok := cookingID(c)
	if !ok {
		return
	}
	var input models.CookingArchiveInput
	if !cookingBind(c, &input) {
		return
	}
	v, e := cookingService(c).Archive(id, input)
	cookingResult(c, v, e)
}

// GetCookingStock handles the workspace-scoped cooking contract.
// @Summary Read finished stock and recent quantity history
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Success 200 {object} models.CookingStock
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-stock [get]
func GetCookingStock(c *gin.Context) {
	v, e := cookingService(c).Stock()
	if e != nil {
		cookingFailure(c, e)
		return
	}
	c.JSON(200, v)
}

// AdjustCookingStock handles the workspace-scoped cooking contract.
// @Summary Record initial stock, return, waste or signed correction
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingAdjustmentInput true "Cooking request; quantities are decimal strings"
// @Success 200 {object} models.CookingStock
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-stock/adjustments [post]
func AdjustCookingStock(c *gin.Context) {
	if !cookingWriteAllowed(c) {
		return
	}
	var input models.CookingAdjustmentInput
	if !cookingBind(c, &input) {
		return
	}
	if (input.Kind == "initial" || input.Kind == "return") && !requireNewCooking(c) {
		return
	}
	v, e := cookingService(c).Adjust(input)
	cookingResult(c, v, e)
}

// GetCookingOperation handles the workspace-scoped cooking contract.
// @Summary Recover a committed operation result
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Param operation_id path string true "Stable operation UUID"
// @Success 200 {object} map[string]interface{}
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking/operations/{operation_id} [get]
func GetCookingOperation(c *gin.Context) {
	v, e := cookingService(c).Receipt(c.Param("operation_id"))
	if e != nil {
		cookingFailure(c, e)
		return
	}
	c.JSON(200, gin.H{"status": "committed", "result": json.RawMessage(v)})
}

// ReplanCookingRun handles the workspace-scoped cooking contract.
// @Summary Explicitly replace an uncooked plan using a fresh quote
// @Tags Cooking
// @Security BearerAuth
// @Produce json
// @Accept json
// @Param request body models.CookingCreateInput true "Cooking request; quantities are decimal strings"
// @Param id path int true "Cooking run ID"
// @Success 200 {object} models.CookingRun
// @Failure 400,401,403,404,409,500,503 {object} map[string]string
// @Router /api/cooking-runs/{id}/plan [put]
func ReplanCookingRun(c *gin.Context) {
	if !cookingWriteAllowed(c) {
		return
	}
	id, ok := cookingID(c)
	if !ok {
		return
	}
	var input models.CookingCreateInput
	if !cookingBind(c, &input) {
		return
	}
	v, e := cookingService(c).Replan(id, input)
	cookingResult(c, v, e)
}

// Tracked order authorization runs under the workspace inventory lock.
var errCookingReadOnly = errors.New("inventory-tracked orders are read-only for this role")

func cookingOrderWriteError(tx *gorm.DB, role string, ws, orderID uint) error {
	if role != "viewer" || !tx.Migrator().HasTable(&models.CookingStockAllocation{}) {
		return nil
	}
	var n int64
	if e := tx.Model(&models.CookingRunOrder{}).Where("workspace_id = ? AND order_id = ?", ws, orderID).Count(&n).Error; e != nil {
		return e
	}
	if n == 0 {
		if e := tx.Model(&models.CookingStockAllocation{}).Where("workspace_id = ? AND order_id = ?", ws, orderID).Count(&n).Error; e != nil {
			return e
		}
	}
	if n > 0 {
		return errCookingReadOnly
	}
	return nil
}
