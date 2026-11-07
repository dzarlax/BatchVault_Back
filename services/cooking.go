package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/big"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"sort"

	"time"
)

type CookingConflict struct {
	Code    string
	Message string
}

func (e *CookingConflict) Error() string         { return e.Message }
func cookingConflict(code, message string) error { return &CookingConflict{code, message} }

type CookingService struct {
	DB          *gorm.DB
	WorkspaceID uint
	UserID      uint
}

func hashCooking(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func stockNumber(s string) (*big.Rat, error) { return cookingRat(s, "quantity", true) }
func stockText(r *big.Rat) string            { s, _ := cookingString(r); return s }
func subtractStock(a, b string) (string, error) {
	x, e := stockNumber(a)
	if e != nil {
		return "", e
	}
	y, e := stockNumber(b)
	if e != nil {
		return "", e
	}
	x.Sub(x, y)
	if x.Sign() < 0 {
		return "", cookingConflict("insufficient_stock", "Insufficient available stock")
	}
	return stockText(x), nil
}
func addStock(a, b string) (string, error) {
	x, e := stockNumber(a)
	if e != nil {
		return "", e
	}
	y, e := stockNumber(b)
	if e != nil {
		return "", e
	}
	return cookingString(x.Add(x, y))
}
func stockState(tx *gorm.DB, ws uint) (models.CookingWorkspaceState, error) {
	v := models.CookingWorkspaceState{WorkspaceID: ws, Version: 1}
	err := tx.Where("workspace_id = ?", ws).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, nil
	}
	return v, err
}
func cookingStockView(tx *gorm.DB, ws uint) (models.CookingStock, error) {
	state, e := stockState(tx, ws)
	if e != nil {
		return models.CookingStock{}, e
	}
	out := models.CookingStock{Version: state.Version, Lots: []models.CookingStockLot{}, Events: []models.CookingEvent{}}
	if e = tx.Where("workspace_id = ?", ws).Order("id ASC").Find(&out.Lots).Error; e != nil {
		return out, e
	}
	if e = tx.Where("workspace_id = ?", ws).Order("id DESC").Limit(200).Find(&out.Events).Error; e != nil {
		return out, e
	}
	for i := range out.Lots {
		out.Lots[i].Available, e = subtractStock(out.Lots[i].Quantity, out.Lots[i].Reserved)
		if e != nil {
			return out, e
		}
	}
	return out, nil
}
func (s CookingService) Stock() (models.CookingStock, error) {
	return cookingStockView(s.DB, s.WorkspaceID)
}
func (s CookingService) Run(id uint) (models.CookingRun, error) { return s.runView(s.DB, id) }
func (s CookingService) runView(tx *gorm.DB, id uint) (models.CookingRun, error) {
	var run models.CookingRun
	if e := tx.Where("id = ? AND workspace_id = ?", id, s.WorkspaceID).First(&run).Error; e != nil {
		return run, e
	}
	if e := s.refreshPlanUses(tx, &run); e != nil {
		return run, e
	}
	run.Events = []models.CookingEvent{}
	run.Lots = []models.CookingStockLot{}
	if e := tx.Where("workspace_id = ? AND run_id = ?", s.WorkspaceID, id).Order("id ASC").Find(&run.Events).Error; e != nil {
		return run, e
	}
	stock, e := cookingStockView(tx, s.WorkspaceID)
	if e != nil {
		return run, e
	}
	run.StockVersion = stock.Version
	for _, l := range stock.Lots {
		if l.RunID == run.ID {
			run.Lots = append(run.Lots, l)
		}
	}
	var current []models.Order
	if len(run.PlanInput.OrderIDs) > 0 {
		if e = tx.Where("workspace_id = ? AND id IN ?", s.WorkspaceID, run.PlanInput.OrderIDs).Preload("Items").Order("id ASC").Find(&current).Error; e != nil {
			return run, e
		}
		original := map[uint]models.CookingOrderLine{}
		for _, l := range run.Plan.OrderLines {
			original[l.OrderItemID] = l
		}
		seen := map[uint]bool{}
		for _, o := range current {
			for _, i := range o.Items {
				old, ok := original[i.ID]
				if ok {
					seen[i.ID] = true
					if old.Quantity != i.Quantity || old.ProductID != i.ProductID || o.Status == "canceled" || o.Status == "finished" {
						run.OrderChanges = append(run.OrderChanges, models.CookingIssue{Code: "order_changed", Message: "Order demand has changed since this cooking was planned", ProductID: i.ProductID})
					}
				}
			}
		}
		for id := range original {
			if !seen[id] {
				run.OrderChanges = append(run.OrderChanges, models.CookingIssue{Code: "order_changed", Message: "An original order item is no longer available"})
			}
		}
	}
	return run, nil
}
func (s CookingService) List(cursor uint) ([]models.CookingRun, *uint, error) {
	rows := []models.CookingRun{}
	q := s.DB.Where("workspace_id = ?", s.WorkspaceID)
	if cursor > 0 {
		q = q.Where("id < ?", cursor)
	}
	if e := q.Order("id DESC").Limit(51).Find(&rows).Error; e != nil {
		return nil, nil, e
	}
	var next *uint
	if len(rows) > 50 {
		n := rows[49].ID
		next = &n
		rows = rows[:50]
	}
	state, e := stockState(s.DB, s.WorkspaceID)
	if e != nil {
		return nil, nil, e
	}
	for i := range rows {
		if e := s.refreshPlanUses(s.DB, &rows[i]); e != nil {
			return nil, nil, e
		}
		rows[i].StockVersion = state.Version
		rows[i].Events = []models.CookingEvent{}
		rows[i].Lots = []models.CookingStockLot{}
	}
	return rows, next, nil
}
func (s CookingService) Receipt(id string) (json.RawMessage, error) {
	var event models.CookingEvent
	if e := s.DB.Where("workspace_id = ? AND operation_id = ?", s.WorkspaceID, id).First(&event).Error; e != nil {
		return nil, e
	}
	return event.Result, nil
}
func (s CookingService) mutate(kind, op string, input any, fn func(*gorm.DB, *models.CookingWorkspaceState) (uint, error)) (json.RawMessage, error) {
	if _, e := uuid.Parse(op); e != nil {
		return nil, cookingError("invalid_operation_id", "operation_id", "A stable UUID operation ID is required")
	}
	digest := hashCooking(input)
	var result json.RawMessage
	e := database.CookingTransaction(s.DB, s.WorkspaceID, func(tx *gorm.DB, state *models.CookingWorkspaceState) error {
		var prior models.CookingEvent
		e := tx.Where("workspace_id = ? AND operation_id = ?", s.WorkspaceID, op).First(&prior).Error
		if e == nil {
			if prior.RequestHash != digest || prior.Kind != kind {
				return cookingConflict("operation_mismatch", "This operation ID was already used for a different request")
			}
			result = prior.Result
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		runID, e := fn(tx, state)
		if e != nil {
			return e
		}
		state.Version++
		if e = tx.Save(state).Error; e != nil {
			return e
		}
		payload, e := json.Marshal(input)
		if e != nil {
			return e
		}
		event := models.CookingEvent{WorkspaceID: s.WorkspaceID, RunID: runID, UserID: s.UserID, OperationID: op, RequestHash: digest, Kind: kind, Payload: payload}
		if e = tx.Create(&event).Error; e != nil {
			return e
		}
		var view any
		if runID == 0 || kind == "adjustment" {
			v, e := cookingStockView(tx, s.WorkspaceID)
			if e != nil {
				return e
			}
			view = v
		} else {
			v, e := s.runView(tx, runID)
			if e != nil {
				return e
			}
			view = v
		}
		result, e = json.Marshal(view)
		if e != nil {
			return e
		}
		return tx.Model(&event).Update("result", result).Error
	})
	return result, e
}
func versionCooking(actual, wanted uint) error {
	if actual != wanted {
		return cookingConflict("stale_version", "This cooking or stock changed; refresh before saving")
	}
	return nil
}
func (s CookingService) sourceLocks(tx *gorm.DB, orderIDs []uint) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	for _, model := range []any{&models.Package{}, &models.Product{}, &models.Recipe{}} {
		if e := tx.Model(model).Clauses(clause.Locking{Strength: "UPDATE"}).Where("workspace_id = ?", s.WorkspaceID).Order("id ASC").Find(model).Error; e != nil {
			return e
		}
	}
	if len(orderIDs) > 0 {
		var orders []models.Order
		return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("workspace_id = ? AND id IN ?", s.WorkspaceID, orderIDs).Order("id ASC").Find(&orders).Error
	}
	return nil
}
func (s CookingService) loadPlan(tx *gorm.DB, input models.CookingPlanInput) (models.CookingPlanPreview, error) {
	out := models.CookingPlanPreview{ActiveRuns: []models.CookingActiveRun{}}
	if len(input.OrderIDs) > 100 || len(input.Extras) > 100 || len(input.StockUses) > 100 {
		return out, cookingError("too_many_rows", "", "At most 100 input rows are supported")
	}
	var ownRun uint
	if input.RunID != nil {
		var ownerRun models.CookingRun
		if err := tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, *input.RunID).First(&ownerRun).Error; err != nil {
			return out, err
		}
		ownRun = ownerRun.ID
	}
	seen := map[uint]bool{}
	for _, id := range input.OrderIDs {
		if id == 0 || seen[id] {
			return out, cookingError("invalid_order", "order_ids", "Orders must be unique positive IDs")
		}
		seen[id] = true
	}
	var orders []models.Order
	if len(input.OrderIDs) > 0 {
		if e := tx.Where("workspace_id = ? AND id IN ?", s.WorkspaceID, input.OrderIDs).Preload("Items").Order("id ASC").Find(&orders).Error; e != nil {
			return out, e
		}
		if len(orders) != len(input.OrderIDs) {
			return out, gorm.ErrRecordNotFound
		}
	}
	for _, o := range orders {
		if o.Status == "finished" || o.Status == "canceled" {
			return out, cookingConflict("order_changed", "Select unfinished, non-canceled orders")
		}
	}
	products := []models.Product{}
	recipes := []models.Recipe{}
	if e := tx.Where("workspace_id = ?", s.WorkspaceID).Preload("Package").Preload("CostComponents").Preload("AssortmentComponents").Order("id ASC").Find(&products).Error; e != nil {
		return out, e
	}
	if e := tx.Where("workspace_id = ?", s.WorkspaceID).Preload("RecipeIngredients", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).Preload("RecipeIngredients.Ingredient").Order("id ASC").Find(&recipes).Error; e != nil {
		return out, e
	}
	raw, e := BuildCookingPlan(orders, products, recipes, input.Extras, input.BaseSelections)
	if e != nil {
		return out, e
	}
	if len(raw.Flavors) == 0 {
		return out, cookingError("empty_plan", "order_ids", "Select orders or add a recipe for make-ahead cooking")
	}
	flavorSet := map[uint]bool{}
	for _, f := range raw.Flavors {
		flavorSet[f.RecipeID] = true
	}
	// Make-ahead cooking still needs package choices, without adding their demand.
	packing := map[uint]models.CookingProductSnapshot{}
	for _, v := range raw.Products {
		packing[v.ID] = v
	}
	for _, p := range products {
		if _, ok := packing[p.ID]; ok {
			continue
		}
		one := models.Order{ID: 0, Items: []models.OrderItem{{ProductID: p.ID, Quantity: 1}}}
		candidate, err := BuildCookingPlan([]models.Order{one}, products, recipes, nil, input.BaseSelections)
		if err != nil {
			continue
		}
		for _, spec := range candidate.Products {
			valid := true
			for _, c := range spec.Components {
				if !flavorSet[c.RecipeID] {
					valid = false
				}
			}
			if valid {
				packing[spec.ID] = spec
			}
		}
	}
	raw.Products = []models.CookingProductSnapshot{}
	for _, v := range packing {
		raw.Products = append(raw.Products, v)
	}
	sort.Slice(raw.Products, func(i, j int) bool { return raw.Products[i].ID < raw.Products[j].ID })
	out.SourceSignature = hashCooking(struct {
		Plan   models.CookingPlan
		Orders []uint
		Extras []models.CookingExtraInput
	}{raw, input.OrderIDs, input.Extras})
	out.Plan = raw
	// Already held or handed-out packages cover the same order item, never another one.
	itemIDs := []uint{}
	for _, l := range raw.OrderLines {
		itemIDs = append(itemIDs, l.OrderItemID)
	}
	coverage := map[uint]int{}
	ownHolds := map[uint]*big.Rat{}
	if ownRun > 0 {
		var held []models.CookingStockAllocation
		if e = tx.Where("workspace_id = ? AND run_id = ? AND state = ?", s.WorkspaceID, ownRun, "held").Find(&held).Error; e != nil {
			return out, e
		}
		for _, a := range held {
			n, e := stockNumber(a.Quantity)
			if e != nil {
				return out, e
			}
			if ownHolds[a.LotID] == nil {
				ownHolds[a.LotID] = new(big.Rat)
			}
			ownHolds[a.LotID].Add(ownHolds[a.LotID], n)
		}
	}
	if len(itemIDs) > 0 {
		var allocs []models.CookingStockAllocation
		if e = tx.Table("cooking_stock_allocations AS a").Select("a.*").Joins("JOIN cooking_stock_lots AS l ON l.id = a.lot_id AND l.workspace_id = a.workspace_id").Joins("JOIN order_items AS i ON i.id = a.order_item_id AND i.product_id = l.product_id AND i.deleted_at IS NULL").Where("a.workspace_id = ? AND a.kind = ? AND a.order_item_id IN ? AND a.state IN ?", s.WorkspaceID, "order", itemIDs, []string{"held", "consumed"}).Find(&allocs).Error; e != nil {
			return out, e
		}
		for _, a := range allocs {
			n, e := stockNumber(a.Quantity)
			if e != nil {
				return out, e
			}
			if !n.IsInt() {
				return out, fmt.Errorf("invalid package allocation")
			}
			if a.RunID != ownRun || a.State != "held" {
				coverage[a.OrderItemID] += int(n.Num().Int64())
			}
		}
	}
	needs := map[uint]*big.Rat{}
	for _, f := range raw.Flavors {
		n, e := stockNumber(f.TargetFinishedG)
		if e != nil {
			return out, e
		}
		needs[f.RecipeID] = n
	}
	specs := map[uint]models.CookingProductSnapshot{}
	for _, p := range raw.Products {
		specs[p.ID] = p
	}
	for _, l := range raw.OrderLines {
		n := coverage[l.OrderItemID]
		if n > l.Quantity {
			n = l.Quantity
		}
		for _, c := range specs[l.ProductID].Components {
			v, _ := stockNumber(c.QuantityG)
			v.Mul(v, big.NewRat(int64(n), 1))
			needs[c.RecipeID].Sub(needs[c.RecipeID], v)
		}
	}
	stock, e := cookingStockView(tx, s.WorkspaceID)
	if e != nil {
		return out, e
	}
	out.Stock = stock
	out.StockVersion = stock.Version
	lots := map[uint]models.CookingStockLot{}
	for _, l := range stock.Lots {
		if held := ownHolds[l.ID]; held != nil {
			free, _ := stockNumber(l.Available)
			l.Available = stockText(free.Add(free, held))
		}
		lots[l.ID] = l
	}
	usedIDs := map[uint]bool{}
	remainingByProduct := map[uint]int{}
	for _, l := range raw.OrderLines {
		n := l.Quantity - coverage[l.OrderItemID]
		if n > 0 {
			remainingByProduct[l.ProductID] += n
		}
	}
	orderedUses := append([]models.CookingStockUse{}, input.StockUses...)
	sort.SliceStable(orderedUses, func(i, j int) bool {
		return lots[orderedUses[i].LotID].Kind == "packaged" && lots[orderedUses[j].LotID].Kind != "packaged"
	})
	for _, use := range orderedUses {
		l, ok := lots[use.LotID]
		if !ok {
			return out, gorm.ErrRecordNotFound
		}
		if usedIDs[l.ID] {
			return out, cookingError("duplicate_source", "stock_uses", "A stock lot can only be selected once")
		}
		usedIDs[l.ID] = true
		qty, e := cookingRat(use.Quantity, "stock_uses.quantity", false)
		if e != nil {
			return out, e
		}
		free, _ := stockNumber(l.Available)
		if qty.Cmp(free) > 0 {
			return out, cookingConflict("insufficient_stock", "Selected stock is no longer available")
		}
		if l.Kind == "bulk" {
			target, ok := needs[l.RecipeID]
			if !ok || qty.Cmp(target) > 0 {
				return out, cookingError("invalid_source", "stock_uses", "Bulk stock exceeds the required flavor amount")
			}
			target.Sub(target, qty)
		} else {
			spec, ok := specs[l.ProductID]
			if !ok || !qty.IsInt() || qty.Num().Int64() > int64(remainingByProduct[l.ProductID]) || hashCooking(spec.Components) != hashCooking(l.Composition) || spec.ConfigurationVersion != l.ConfigurationVersion {
				return out, cookingError("invalid_source", "stock_uses", "Select compatible whole packages needed by the selected orders")
			}
			remainingByProduct[l.ProductID] -= int(qty.Num().Int64())
			for _, c := range spec.Components {
				v, _ := stockNumber(c.QuantityG)
				v.Mul(v, qty)
				needs[c.RecipeID].Sub(needs[c.RecipeID], v)
			}
		}
	}
	for i := range out.Plan.Flavors {
		f := &out.Plan.Flavors[i]
		n := needs[f.RecipeID]
		if n.Sign() < 0 {
			n.SetInt64(0)
		}
		original, _ := stockNumber(f.TargetFinishedG)
		if f.SuggestedMeatG != nil && original.Sign() > 0 {
			v, _ := stockNumber(*f.SuggestedMeatG)
			v.Mul(v, new(big.Rat).Quo(n, original))
			text := stockText(v)
			f.SuggestedMeatG = &text
		}
		f.TargetFinishedG = stockText(n)
	}
	if len(input.OrderIDs) > 0 {
		var links []models.CookingRunOrder
		if e = tx.Where("workspace_id = ? AND order_id IN ?", s.WorkspaceID, input.OrderIDs).Find(&links).Error; e != nil {
			return out, e
		}
		runIDs := []uint{}
		for _, l := range links {
			runIDs = append(runIDs, l.RunID)
		}
		if len(runIDs) > 0 {
			var runs []models.CookingRun
			if e = tx.Where("workspace_id = ? AND id IN ? AND state <> ?", s.WorkspaceID, runIDs, "archived").Order("id ASC").Find(&runs).Error; e != nil {
				return out, e
			}
			for _, r := range runs {
				if r.ID != ownRun {
					out.ActiveRuns = append(out.ActiveRuns, models.CookingActiveRun{ID: r.ID, Title: r.Title})
				}
			}
		}
	}
	return out, nil
}
func (s CookingService) Plan(input models.CookingPlanInput) (models.CookingPlanPreview, error) {
	var out models.CookingPlanPreview
	e := database.CostingTransaction(s.DB, true, func(tx *gorm.DB) error { v, e := s.loadPlan(tx, input); out = v; return e })
	return out, e
}
func (s CookingService) Create(input models.CookingCreateInput) (json.RawMessage, error) {
	return s.mutate("plan", input.OperationID, input, func(tx *gorm.DB, state *models.CookingWorkspaceState) (uint, error) {
		if input.RunID != nil {
			return 0, cookingError("invalid_run_id", "run_id", "New cooking cannot reuse another cooking reservation")
		}
		if e := versionCooking(state.Version, input.StockVersion); e != nil {
			return 0, e
		}
		if e := s.sourceLocks(tx, input.OrderIDs); e != nil {
			return 0, e
		}
		preview, e := s.loadPlan(tx, input.CookingPlanInput)
		if e != nil {
			return 0, e
		}
		if preview.SourceSignature != input.SourceSignature {
			return 0, cookingConflict("stale_source", "Recipe, product or order changed; calculate the plan again")
		}
		acknowledged := map[uint]bool{}
		for _, id := range input.AcknowledgedRunIDs {
			acknowledged[id] = true
		}
		for _, run := range preview.ActiveRuns {
			if !acknowledged[run.ID] {
				return 0, cookingConflict("already_planned", "These orders are already part of another cooking; review and acknowledge it")
			}
		}
		date := time.Now()
		if input.Date != "" {
			date, e = time.Parse(time.RFC3339, input.Date)
			if e != nil {
				date, e = time.Parse("2006-01-02", input.Date)
			}
			if e != nil {
				return 0, cookingError("invalid_date", "date", "Use an ISO calendar date or RFC3339 timestamp")
			}
		}
		if len(input.Title) > 200 {
			return 0, cookingError("invalid_title", "title", "Title must be at most 200 characters")
		}
		if input.Title == "" {
			input.Title = "Cooking " + date.Format("2006-01-02")
		}
		run := models.CookingRun{WorkspaceID: s.WorkspaceID, UserID: s.UserID, Date: date, Title: input.Title, State: "draft", Version: 1, SourceSignature: preview.SourceSignature, Plan: preview.Plan, PlanInput: input.CookingPlanInput, ActualOutput: []models.CookingComponent{}}
		if e = tx.Create(&run).Error; e != nil {
			return 0, e
		}
		for _, f := range run.Plan.Flavors {
			if e = tx.Create(&models.CookingRunFlavor{WorkspaceID: s.WorkspaceID, RunID: run.ID, RecipeID: f.RecipeID, ActualMeatG: "0", RecipeSnapshot: f.RecipeSnapshot}).Error; e != nil {
				return 0, e
			}
		}
		for _, l := range run.Plan.OrderLines {
			if e = tx.Create(&models.CookingRunOrder{WorkspaceID: s.WorkspaceID, RunID: run.ID, OrderID: l.OrderID, OrderItemID: l.OrderItemID, Quantity: l.Quantity}).Error; e != nil {
				return 0, e
			}
		}
		if e = s.holdPlanStock(tx, &run, input.StockUses); e != nil {
			return 0, e
		}
		return run.ID, nil
	})
}
func (s CookingService) PreparationPreview(input models.CookingPreparationInput) (models.CookingPreparation, error) {
	var plan models.CookingPlan
	if input.RunID != nil {
		run, e := s.Run(*input.RunID)
		if e != nil {
			return models.CookingPreparation{}, e
		}
		plan = run.Plan
	} else {
		if input.RecipeID == nil {
			return models.CookingPreparation{}, cookingError("missing_recipe", "recipe_id", "A recipe or cooking run is required")
		}
		v, e := s.Plan(models.CookingPlanInput{Extras: []models.CookingExtraInput{{RecipeID: *input.RecipeID, FinishedG: "0"}}})
		if e != nil {
			return models.CookingPreparation{}, e
		}
		plan = v.Plan
	}
	return CalculateCookingPreparation(plan, input)
}
func (s CookingService) Prepare(id uint, input models.CookingPreparationInput) (json.RawMessage, error) {
	return s.mutate("preparation", input.OperationID, struct {
		RunID uint
		Input models.CookingPreparationInput
	}{id, input}, func(tx *gorm.DB, state *models.CookingWorkspaceState) (uint, error) {
		var run models.CookingRun
		if e := tx.Where("id = ? AND workspace_id = ?", id, s.WorkspaceID).First(&run).Error; e != nil {
			return 0, e
		}
		if e := versionCooking(run.Version, input.Version); e != nil {
			return 0, e
		}
		if run.State == "cooked" || run.State == "archived" {
			return 0, cookingConflict("state_conflict", "Preparation is immutable after output is recorded")
		}
		if e := s.sourceLocks(tx, run.PlanInput.OrderIDs); e != nil {
			return 0, e
		}
		sourceInput := run.PlanInput
		sourceInput.StockUses = nil
		source, e := s.loadPlan(tx, sourceInput)
		if e != nil {
			return 0, e
		}
		if source.SourceSignature != run.SourceSignature || input.SourceSignature != run.SourceSignature {
			return 0, cookingConflict("stale_source", "The recipe or order changed; review a new cooking plan")
		}
		prep, e := CalculateCookingPreparation(run.Plan, input)
		if e != nil {
			return 0, e
		}
		for _, f := range prep.Flavors {
			for _, ri := range f.RecipeSnapshot.Ingredients {
				if e = database.RequireWorkspaceIngredient(tx, s.WorkspaceID, ri.IngredientID); e != nil {
					return 0, cookingError("inactive_ingredient", "ingredient_id", "A recipe ingredient is not active in this workspace")
				}
			}
		}
		run.Preparation = &prep
		run.State = "prepared"
		run.Version++
		if e = tx.Omit("Events", "Lots").Save(&run).Error; e != nil {
			return 0, e
		}
		for _, f := range prep.Flavors {
			if e = tx.Model(&models.CookingRunFlavor{}).Where("workspace_id = ? AND run_id = ? AND recipe_id = ?", s.WorkspaceID, id, f.RecipeID).Updates(map[string]any{"actual_meat_g": f.ActualMeatG, "recipe_snapshot": mustCookingJSON(f.RecipeSnapshot)}).Error; e != nil {
				return 0, e
			}
		}
		return id, nil
	})
}
func mustCookingJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
