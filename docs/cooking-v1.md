# Cooking v1 and finished stock

Cooking v1 follows orders → approximate finished demand → actual prepared main ingredient
by flavor → scaled marinades → usable output → one or more packing operations.
It is separate from legacy cooking sessions and ingredient inventory. A run can
include orders, make-ahead grams, or both. No purchase quantity, waste ledger, or
immediate packing is mandatory. Usable output is measured after nonconforming
pieces are excluded. Historical order costs and manual costing remain unchanged.

## Capability and rollout

`COOKING_ENABLED` defaults to false. When true, workspace responses advertise
`capabilities.cooking_v1: true` (the capabilities value is an object). New run
creation, plan previews, standalone preparation calculation, and initial/return
stock entry require the flag. Existing run lifecycle, stock reconciliation,
operation receipts and order stock hooks remain available when disabled.
Viewers may read and preview, but cannot write cooking data or change tracked
orders through either legacy order update endpoint or deletion.

Deploy the additive backend schema before enabling clients. Enable the flag only
after separate deployment approval and an isolated acceptance run. This change
contains no deployment or production data migration execution.

## Contract

All endpoints are under `/api`, JWT-authenticated and scoped by workspace
middleware (`X-Workspace-ID`, with the existing personal-workspace default).
Quantities in the new contract are decimal **strings**, precision at most 0.001;
pack counts are whole numbers. The existing order/package APIs retain their
formats. Dates accept `YYYY-MM-DD` or RFC3339; responses use RFC3339.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/cooking/plan-preview` | Read-only demand, main-ingredient weight guidance, stock and active-run warning |
| POST | `/cooking-runs` | Create a plan and reserve explicitly selected stock |
| GET | `/cooking-runs?cursor=…` | Up to 50 newest runs; string `next_cursor` or null |
| GET | `/cooking-runs/:id` | Frozen plan, preparation, output, history, current lots/version |
| PUT | `/cooking-runs/:id/plan` | Explicit replan before cooked output, clearing preparation |
| POST | `/cooking/preparation-preview` | Actual main ingredient and scaled ingredients, without persistence |
| PUT | `/cooking-runs/:id/preparation` | Save main ingredient allocation and recipe snapshots |
| POST | `/cooking-runs/:id/packing-preview` | Read-only packing consumption and shortages |
| POST | `/cooking-runs/:id/results` | Usable output, optionally with atomic first packing |
| POST | `/cooking-runs/:id/packings` | Subsequent partial packing |
| POST | `/cooking-runs/:id/archive` | Archive and release source holds; preserve order reservations and history |
| GET | `/cooking-stock` | Lot balances, reservations, available amounts and last 200 events |
| POST | `/cooking-stock/adjustments` | Initial stock, returns, waste, signed correction |
| GET | `/cooking/operations/:operation_id` | Recover a committed result after an unknown outcome |

The source plan input contains `order_ids`, `extras` (`recipe_id`, `finished_g`),
optional explicit base-row selections, selected stock lots and quantities, and
acknowledged active-run IDs. Replan previews also include `run_id` so the run's
own held stock is available without hiding already consumed coverage. Create
rejects `run_id`. Create/replan bodies are flat: plan fields plus `title`, `date`,
`source_signature`, `stock_version`, `operation_id` (and `version` for replan).

A read run exposes `plan_input` preserving make-ahead extras and projecting only
**remaining held** quantities from its original selected source lots. Consumed
and released selections cannot become fresh reservations when replanning.
Clients must preview again and explicitly confirm replan. Replanning after usable
output is rejected. Catalog changes do not rewrite cooked specifications.

Preparation requires every planned flavor exactly once, including explicit zero
for unused flavors. The selected base must be a recipe ingredient of type `base`
with a mass unit; ambiguous bases require a choice. This can be any mass-based
main ingredient, selected by recipe role rather than name or food category.
Existing `*_meat_g` JSON field names represent this main ingredient weight in
grams and remain unchanged for client compatibility. `prepared_meat_g` need not
all be allocated. Per-flavor `ingredients` contains marinade ingredients; combined
`ingredients_total` includes the actual main ingredient normalized to grams. Grams and
milliliters are never added together or converted without a density.

The actual result uses `actual_usable_g` entries, one per flavor, including zero.
Packing uses product IDs from the frozen plan and explicit source lots where
needed. Ordinary packs consume their recipe grams; assortment packs consume the
frozen component grams. Missing quantities or negative balances fail atomically.
Initial/return adjustments create a new lot using exactly one recipe ID (grams)
or product ID (whole packs). Waste subtracts a positive magnitude from a lot.
Correction applies a **signed delta**, not a replacement balance. Reason is
required. Reserved quantities cannot be removed by correction or waste.

## Persistence and concurrency

Seven additive tables store runs, flavor snapshots, linked order items,
workspace stock version, lots, allocations and operation events. There is no
legacy backfill and no reinterpretation of historical costs. Database constraints
prevent negative quantities or reservations beyond lot quantity.

Inventory mutation first locks the workspace state row, then run/order/source
rows as needed. Catalog snapshots lock packages, products and recipe parents;
recipe ingredient writers use the same parent lock. Inventory writes use READ
COMMITTED. Legacy full order edits retain REPEATABLE READ for a consistent cost
graph, plus the workspace lock and bounded retries for PostgreSQL serialization
or deadlock errors. Tracked viewer authorization runs inside this transaction.

Every physical mutation has a stable UUID `operation_id`. The event receipt,
quantity changes, snapshots and stock-version increment commit atomically.
Replay with the same operation and payload returns the stored result before
checking stale versions. Reusing an ID with another payload/path is a conflict.
A missing operation receipt is HTTP404; a committed lookup returns
`{ "status": "committed", "result": … }`. Clients persist the exact body and
scope it by server, account and workspace; context switches cannot replay it
elsewhere. After receipt recovery, clients refetch current state.

Held package allocations are tied to order item and product. Finishing an order
consumes held packages once; repeated finish calls or reopening do not restock
previously issued packages. Cancel/delete/item removal releases remaining holds.
Changing the product on a reused item ID does not inherit another product's
consumed coverage. Returns require an explicit stock event. Legacy untracked
orders retain their existing stock-free behavior. Cooking writes emit no push;
legacy order notifications occur only after a committed transition.

## Verification and rollback

Pure tests cover exact recipe scaling, unit aggregation, yield guidance and
assortment consumption. Store tests exercise real PostgreSQL when
`COSTING_TEST_DATABASE_URL` names a disposable localhost database named
`costing_test`; otherwise nonconcurrent tests use isolated SQLite. Tests create
and drop unique schemas. Controller tests exercise mutation replay, both order
completion routes, workspace isolation, viewer restrictions and feature rollback.

Run affected suites with `go test ./services ./controllers ./database`. The
PostgreSQL suite additionally checks concurrent packing and a catalog commit
between order reads. Web/mobile clients have separate transport and UI checks.

To stop new work, disable `COOKING_ENABLED` while keeping this backend version's
ledger and order hooks. Hide new-entry UI through capability refresh. Preserve
all ledger tables and receipts; do not drop tables or revert to an older backend
that ignores outstanding holds/issued quantities. Correct stock through explicit
reasoned adjustments. Deployment and destructive schema rollback require
separate review and approval.
