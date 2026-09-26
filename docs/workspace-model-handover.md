# Workspace Model Handover

Date: 2026-05-28  
Backend snapshot reviewed: `1f5a853`  
Repository: `BatchVault_Back`

## Purpose

This document hands over the workspace/account data model discussion for backend implementation planning. It started from the ingredients screen redesign, but the discussion exposed a broader ownership problem: production data is currently scoped mostly by `user_id`, while ingredients are global. That makes the ingredients and pricing UX break down when different users on the same installation operate different businesses or domains, such as jerky production and cake production.

The proposed direction is to introduce `workspaces` as the main boundary for operational data. Users remain identities. Workspaces own production data. Accounts or companies can be added above workspaces later.

## Current Backend State

The backend currently uses JWT authentication with a `userID` claim. The middleware stores that value in Gin context as `userID`.

Most operational entities are scoped by `user_id`:

- `recipes`
- `prices`
- `clients`
- `products`
- `packages`
- `orders`
- `cooking_sessions`
- `product_options`

`ingredients` are different:

- `ingredients` have no `user_id`.
- `ingredients.name` is globally unique.
- `GET /api/ingredients` returns the full global ingredient list.
- `POST /api/ingredients` creates a global ingredient.
- `prices` reference `ingredient_id` and are currently scoped by `user_id`.
- Price updates are modeled as history: `POST /api/prices` creates a new price row instead of overwriting the previous one.

This means the installation has one shared ingredient dictionary, but every authenticated user has separate prices and separate operational data.

## Problem

The current model becomes confusing when multiple users share one installation but operate unrelated production domains.

Example:

- User A produces meat products.
- User B produces cakes.
- Both users share the same installation.

Because `GET /api/ingredients` returns every global ingredient, both users see a mixed list of meat, spices, cake fillings, frosting ingredients, decorations, and anything else created by anyone. Price filters such as "missing price" or "stale price" also become confusing, because they are meaningful only for the user's actual working ingredient set.

The frontend can hide some of this, but it cannot solve the model problem alone. The backend needs a stable boundary for "the data this user is working with right now."

## Target Concept

Use `workspace` as the operational data boundary.

High-level model:

```text
Account / Company
  has many Workspaces

User
  joins Workspaces through WorkspaceMember

Workspace
  owns operational data:
    workspace ingredients
    price history
    recipes
    products
    packages
    clients
    orders
    cooking sessions

Ingredient
  remains a shared installation-wide dictionary entry
```

`Account` or `Company` does not need to be implemented in the first phase. The important decision is to avoid making the user the permanent data owner. A personal workspace can be created automatically for each existing user, then later attached to an account/company if needed.

## Proposed Entities

### Workspace

Represents a working context such as:

- Personal workspace
- Jerky production
- Cakes
- Test kitchen
- A company-owned production workspace

Suggested fields:

```go
type Workspace struct {
    ID        uint
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt

    Name      string
    Slug      string
    AccountID *uint // nullable for the first phase
}
```

### WorkspaceMember

Represents user access to a workspace.

Suggested fields:

```go
type WorkspaceMember struct {
    ID          uint
    CreatedAt   time.Time
    UpdatedAt   time.Time
    DeletedAt   gorm.DeletedAt

    WorkspaceID uint
    UserID      uint
    Role        string // owner, manager, operator, viewer
}
```

Required constraints:

- Unique `(workspace_id, user_id)` for active memberships.
- Index on `user_id`.
- Index on `workspace_id`.

### WorkspaceIngredient

Represents an ingredient being part of a workspace's active working set. This is not a new ingredient. It is a membership/visibility layer between a workspace and the shared ingredient dictionary.

Suggested fields:

```go
type WorkspaceIngredient struct {
    ID           uint
    CreatedAt    time.Time
    UpdatedAt    time.Time
    DeletedAt    gorm.DeletedAt

    WorkspaceID  uint
    IngredientID uint
    Active       bool
    Alias        string // optional local display name
    Category     string // optional workspace-local grouping
}
```

Required constraints:

- Unique `(workspace_id, ingredient_id)` for active membership.
- Index on `(workspace_id, active)`.
- Index on `ingredient_id`.

## Ownership Map

### Identity and access

These are not production records:

- `users`: identity/login.
- `workspace_members`: maps users to workspaces.
- `accounts`: optional future owner above workspaces.

### Shared installation-wide dictionary

These should remain shared unless a separate domain decision changes this:

- `ingredients`: global dictionary entries with name/type.

Potential future extension:

- Ingredient aliases/synonyms to handle localized names, brand names, and duplicate-like entries.

### Workspace-owned operational data

These should move from `user_id` ownership to `workspace_id` ownership:

- `prices`
- `recipes`
- `products`
- `packages`
- `clients`
- `orders`
- `cooking_sessions`

### Join/detail records

These can often inherit workspace through their parent, but controllers must enforce that references do not cross workspace boundaries:

- `recipe_ingredients`: parent recipe is workspace-owned; referenced ingredient should be in `workspace_ingredients`.
- `product_options`: parent product and referenced recipe must belong to the same workspace.
- `order_items`: parent order and referenced product must belong to the same workspace.
- `cooking_session_ingredients`: parent cooking session is workspace-owned; referenced ingredient should be in `workspace_ingredients`.

## Request Context

The current request context only has `userID`. Workspace-aware endpoints need a current workspace.

Recommended approach:

1. Keep `userID` in JWT.
2. Resolve current workspace per request.
3. Validate that the user is a member of that workspace.
4. Put both values in Gin context:
   - `userID`
   - `workspaceID`

Possible ways to select the current workspace:

### Header-based

Use `X-Workspace-ID`.

Pros:

- Minimal route churn.
- Works with existing `/api/...` structure.

Cons:

- Easy for clients to omit.
- Every protected endpoint needs consistent middleware behavior.

### Path-based

Use `/api/workspaces/:workspace_id/...`.

Pros:

- Scope is explicit in URLs.
- Easier to reason about and test.

Cons:

- Larger route change.
- Frontend API calls need broader rewrites.

### Default workspace fallback

If no workspace is specified, use the user's default personal workspace.

Pros:

- Smooth migration.
- Existing frontend can continue working while endpoints are migrated.

Cons:

- Can hide missing workspace selection bugs.

Recommendation: use a default workspace fallback during migration, but make workspace selection explicit in new UI and new API calls.

## Migration Strategy

### Phase 1: Workspace foundation

Add:

- `workspaces`
- `workspace_members`

Backfill:

- Create one personal workspace per existing user.
- Create one owner membership per user.

Middleware/helpers:

- Add helper to resolve current workspace.
- Add helper to validate workspace membership.
- Add tests for membership rejection.

Do not migrate all business tables yet in this phase unless the change is small enough to verify thoroughly.

### Phase 2: Add `workspace_id` to user-owned tables

Add nullable `workspace_id` columns first:

- `prices.workspace_id`
- `recipes.workspace_id`
- `products.workspace_id`
- `packages.workspace_id`
- `clients.workspace_id`
- `orders.workspace_id`
- `cooking_sessions.workspace_id`

Backfill each row from its current `user_id` using the user's personal workspace.

Then update controllers to filter by `workspace_id` after membership validation.

Keep `user_id` temporarily if needed for rollback and audit, but the application should stop relying on it as the primary data boundary.

### Phase 3: Ingredient workspace set

Add `workspace_ingredients`.

Backfill workspace ingredient memberships from existing usage:

- Any ingredient with a price in the workspace.
- Any ingredient used by a recipe in the workspace.
- Any ingredient used by a cooking session in the workspace.

Change ingredients API behavior:

- Main ingredients list returns `workspace_ingredients` joined to `ingredients`, with latest workspace price state.
- Global dictionary search remains available for "add to workspace" flow.
- Creating a new ingredient creates a shared dictionary entry, then creates a workspace membership.

### Phase 4: Account/company layer

Add when team/company ownership is actually needed:

- `accounts`
- `account_members` if account-level access differs from workspace-level access
- `workspaces.account_id`

Personal workspaces can remain accountless or be attached to a personal account.

## Ingredients UX Implications

The ingredients screen should not show all installation-wide ingredients by default.

Default view:

- Shows only `workspace_ingredients`.
- Search is scoped to the workspace ingredient set.
- Filters separate ingredient type from price state:
  - Type: all, base, spice, sauce.
  - Price state: all, missing, stale, current.

Primary action:

- Update price.

Secondary action:

- Add to workspace.

Add-to-workspace flow:

1. Search the shared ingredient dictionary.
2. If a matching ingredient exists, add it to the current workspace.
3. If no matching ingredient exists, create the shared ingredient and add it to the current workspace.
4. Optionally prompt for the first price, but keep ingredient membership and price history as separate records.

IDs should not be displayed in the primary ingredient list. Technical IDs may be useful in admin/debug views, but they are visual noise for normal users.

## API Shape Suggestions

These are illustrative; exact paths can be adjusted.

### Workspace endpoints

```text
GET    /api/workspaces
POST   /api/workspaces
GET    /api/workspaces/:id
PATCH  /api/workspaces/:id
GET    /api/workspaces/:id/members
POST   /api/workspaces/:id/members
```

### Workspace ingredient endpoints

```text
GET    /api/workspace-ingredients
POST   /api/workspace-ingredients
PATCH  /api/workspace-ingredients/:id
DELETE /api/workspace-ingredients/:id
```

Alternative path-based version:

```text
GET    /api/workspaces/:workspace_id/ingredients
POST   /api/workspaces/:workspace_id/ingredients
PATCH  /api/workspaces/:workspace_id/ingredients/:id
DELETE /api/workspaces/:workspace_id/ingredients/:id
```

### Ingredient dictionary endpoints

```text
GET    /api/ingredients/search?query=
POST   /api/ingredients
```

Important distinction:

- Workspace ingredient endpoints manage membership in a workspace.
- Ingredient dictionary endpoints manage shared dictionary entries.

### Price endpoints

```text
GET    /api/prices?ingredient_id=
POST   /api/prices
```

With workspace context:

- `GET /api/prices` returns prices for the current workspace.
- `POST /api/prices` creates a new price history row for the current workspace.
- It must verify that the ingredient belongs to the current workspace, or add it explicitly before accepting a price.

## Query and Index Considerations

Expected indexes:

```sql
CREATE INDEX IF NOT EXISTS idx_workspace_members_user_id
ON workspace_members(user_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_members_workspace_user
ON workspace_members(workspace_id, user_id)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_workspace_ingredients_workspace_active
ON workspace_ingredients(workspace_id, active);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_ingredients_workspace_ingredient
ON workspace_ingredients(workspace_id, ingredient_id)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_prices_workspace_ingredient_date
ON prices(workspace_id, ingredient_id, date DESC);
```

For large ingredient sets, the workspace ingredients list should avoid loading unbounded global ingredients. It should support:

- Search query.
- Type filter.
- Price state filter.
- Pagination or limit/offset.
- Sorting.

## Data Integrity Rules

Required invariants:

- A user can only access a workspace if a valid `workspace_members` row exists.
- Workspace-owned records must always be filtered by `workspace_id`.
- A price can only be created for an ingredient that is present in the current workspace set.
- A recipe can only reference ingredients that are present in the same workspace set.
- A product option can only reference recipes from the same workspace.
- An order item can only reference products from the same workspace.
- A cooking session can only reference recipes and ingredients from the same workspace.

These checks should be centralized in helpers where possible. Avoid repeating ad hoc ownership checks in every controller.

## Compatibility and Rollback

Recommended compatibility strategy:

- Add `workspace_id` columns as nullable first.
- Backfill rows.
- Deploy code that reads `workspace_id` but can fall back to personal workspace where needed.
- After verification, make `workspace_id` required for migrated tables.

Rollback considerations:

- Keep `user_id` columns during the initial migration.
- Do not drop old indexes immediately.
- Log current workspace resolution failures before making them hard failures.

## Main Risks

### Partial migration risk

The largest risk is mixing `user_id` and `workspace_id` ownership in the same workflow. For example, a recipe query by workspace but price lookup by user can silently produce wrong costs.

Mitigation:

- Migrate prices early.
- Add integration tests for recipe cost calculations under multiple workspaces.

### Global ingredient name uniqueness

`ingredients.name` is currently globally unique. That may be too strict when multiple domains and languages share one installation.

Mitigation options:

- Keep it for now, but document the limitation.
- Add normalized names and aliases later.
- Consider uniqueness on normalized name plus type if conflicts become real.

### Authorization drift

Every endpoint currently knows how to validate `user_id`; workspace authorization will need the same rigor.

Mitigation:

- Add common helpers:
  - `RequireWorkspace(c)`
  - `CanAccessWorkspace(userID, workspaceID)`
  - `ScopedDB(c)` or equivalent.

### Frontend migration scope

The frontend currently assumes API calls are globally scoped by token. Workspace context will require either a current workspace selector or default workspace bootstrap.

Mitigation:

- Bootstrap current workspace in auth/session initialization.
- Store current workspace selection in frontend state.
- Send workspace context consistently.

## Open Questions

1. Should workspace selection be header-based, path-based, or both during migration?
2. Should `prices.user_id` remain as "created_by_user_id" after `workspace_id` is introduced?
3. Should recipes/products/clients/orders be migrated in one backend release or phased by module?
4. How should "stale price" be defined: fixed age threshold, workspace setting, or user-configurable?
5. Should shared ingredient creation require admin/review rights later, or can every workspace member create dictionary entries?
6. Should ingredient aliases be workspace-local from day one, or deferred?

## Recommended Next Step

Create a backend implementation plan before changing production code. The first implementation plan should cover only the foundation:

1. Add `Workspace` and `WorkspaceMember` models.
2. Add default personal workspace backfill.
3. Add workspace resolution and membership validation helpers.
4. Add workspace indexes.
5. Add tests for membership enforcement.
6. Decide whether the current frontend can rely on default workspace fallback during the first backend release.

After that foundation is verified, plan the ownership migration for `prices`, then the ingredients workbench.
