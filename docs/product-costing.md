# Recipe and fixed-assortment product costing

Existing products remain manual until explicitly configured. A calculated amount estimates ingredients and one final package; labour, energy, delivery and other overheads are excluded.

## Inputs and formulas

Recipes declare the planned finished yield in grams for their listed ingredient quantities. Packaging declares net finished-content grams and the cost of one complete package. Unknown values are null. A recorded zero price or packaging cost is valid.

An ordinary product selects one costing recipe:

`ingredient batch cost / planned finished grams * package net grams + one package cost`

An assortment assigns fixed positive grams to at least two distinct recipes. Their exact sum must equal the package net weight:

`sum(recipe ingredient batch cost / recipe finished grams * assigned grams) + one package cost`

For synthetic example values, recipe A costs 900 and yields 300 g. A 100 g product with packaging costing 10 has cost 310. Combining 50 g A with 50 g of a recipe costing 600 per 300 g yields 260 including one package costing 10. Five flavours of 50 g use one common 250 g package. Retail packaging from other products is never included in those components. Alternatives, inner packages and nested sets are outside this model.

Ingredient quantities and purchase units must be compatible. Recorded packaging ingredients make a costing recipe incomplete until a content-only recipe is selected. They remain in the explicitly labelled known ingredient subtotal for legacy recipe views.

## Exact amounts and readiness

New weights use `numeric(14,3)` and packaging money uses `numeric(14,2)`. Presence-aware decimal input distinguishes omission, null and zero. The calculation uses exact base-ten rationals, retaining component precision and rounding the final sale-unit amount once to two decimals, half away from zero. Legacy monetary columns are unchanged. Currency is the current workspace currency; these calculations do not convert amounts or change the workspace currency contract.

An incomplete estimate has a null authoritative total and structured issues. It cannot supply a new calculated order cost, profit or margin, and never falls back to the old manual value. Database failures return server errors. Known ingredient subtotals are labelled as such.

## Configuration and historical orders

Product `cost_mode` is `manual`, `recipe` or `assortment`. Legacy recipe links do not select a mode. Manual cost is a complete sale-unit amount and packaging is not added again. `POST /api/products/cost-preview` calculates a draft without writes or notifications. Recipe/package PATCH and product write contracts are documented in `API_DOCUMENTATION.md` and generated Swagger.

New or replaced calculated order rows are recomputed by the server. The displayed `cost_price` is an assertion: a changed rounded amount or incomplete estimate returns 409 and rolls back the entire write. Explicit review of the fresh estimate is required. The server stores the amount and a versioned JSONB source/input snapshot. Manual overrides can include zero.

Saved item IDs preserve row identity. Unchanged historical costs survive quantity/status edits, current product mode changes and product deletion. Calculated historical cost cannot be overridden; legacy rows without a snapshot remain historical manual rows. Ambiguous ID-less calculated matches and unknown snapshot versions are rejected. PUT retains a success-message response followed by GET; a failed read after confirmed save must never repeat the write.

## Transactions and migration

Product/order costing reads and writes use a repeatable-read PostgreSQL snapshot. Package mutation intentionally uses read committed after acquiring the shared package row lock, so usage/composition checks see product links committed while the lock was awaited. Unknown used-package weight initialization requires confirmation and matching existing assortment grams. An established used weight cannot change or be cleared; another size requires another format.

The additive migration preserves manual costs and can run repeatedly. The active product/recipe component pair is unique. Keep additive columns and snapshots during rollback; do not drop history or silently downgrade calculated products.

## Validation and release boundary

Run `go test ./...` and `go vet ./...`. Optional PostgreSQL migration/decimal and package concurrency tests accept only a disposable localhost database named `costing_test`, selected by `COSTING_TEST_DATABASE_URL`; each uses its own schema. Client repositories contain quote-refresh, history-restoration and scoped-recovery tests. The web scenarios cover both order surfaces through stale 409, explicit refresh and successful save.

Calculated modes require compatible backend and client versions. Existing manual products are not automatically converted. Real recipe yields, package values and assortment assignments must be entered explicitly; neither labels nor account data are used to infer them.
