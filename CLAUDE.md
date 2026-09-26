# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## ⚠️ CRITICAL RULE: ENGLISH ONLY

**ALL code, comments, documentation, and text MUST be in English.**

This is a strict requirement. When working with this codebase:
- ✅ Write ALL code comments in English
- ✅ Write ALL documentation in English
- ✅ Write ALL variable/function names in English (transliterated if needed)
- ✅ Write ALL API error messages in English
- ✅ Write ALL Swagger documentation in English
- ✅ Write ALL Git commit messages in English
- ❌ NO Russian or other non-English text allowed anywhere

Before committing changes, verify: `grep -r "[а-яА-ЯёЁ]" **/*.go` should return 0 results.

## Project Overview

BatchVault Backend is a REST API server for a small food production management system. It handles recipes, ingredients, cooking sessions, products, clients, orders, pricing, and dashboard analytics. The application is designed as a multi-tenant system where all data is scoped per user.

**Tech Stack:**
- Go 1.23
- Gin web framework
- PostgreSQL with GORM (pgx driver)
- JWT authentication (24-hour token expiration)
- Swagger API documentation
- Rate limiting middleware
- Input validation

**Module name:** `mobile-backend-go`

## Development Commands

### Running the Application

```bash
# Run directly (requires DATABASE_URL, FRONT_URL, and JWT_SECRET env vars)
go run main.go

# Run through the frontend repository's reusable dev compose.
# This mounts backend Go files and runs `go run .` with cached Go module/build volumes.
# The compose service intentionally uses `sh -c`, not `sh -lc`, because Alpine
# login shell mode resets PATH and can hide /usr/local/go/bin.
docker compose -f ../BatchVault_Front/docker-compose.dev.yml up backend frontend

# After backend source changes, restart the Go runner without rebuilding an image.
docker compose -f ../BatchVault_Front/docker-compose.dev.yml restart backend

# Production-like backend image check from the frontend repository.
docker compose -f ../BatchVault_Front/docker-compose.dev.yml --profile image up -d --build backend-image

# Build Docker image
docker build -t batchvault-back .
```

### Frontend Repository Dev Compose Workflow

The frontend repository keeps an ignored `docker-compose.dev.yml` for local
full-stack development. Its `backend` service mounts a backend git worktree and
runs `go run .` directly from mounted Go files. Use this for quick backend
iteration without rebuilding a Docker image.

When moving to a new backend branch, update the ignored frontend
`docker-compose.dev.yml` backend volume to point at the new backend worktree:

```yaml
services:
  backend:
    volumes:
      - ./_worktrees/<new-backend-worktree>:/app
```

Backend source commits, pushes, and pull requests must be made from the backend
repository/worktree, not from the frontend repository. The production backend
image is updated by backend CI after the backend PR is merged. Use
`backend-image` only for a local production-like image check.

### Swagger Documentation

Generate/update Swagger docs:
```bash
swag init -g main.go
```

Access Swagger UI at: `http://localhost:8080/swagger/index.html`

## Architecture

### Database Layer

All database operations use the global `database.DB` *gorm.DB instance. Auto-migration runs on startup in `database.ConnectDatabase()` for all models defined in `models/`.

**Database Optimization:**
- Indexes are automatically created on startup via `createIndexes()` function
- Indexes cover: workspace membership/default workspace lookup, orders (user_id, client_id, status, created_at), order_items, products, clients, recipes, ingredients, prices, cooking_sessions, recipe_ingredients
- Indexes use `IF NOT EXISTS` for safe repeated execution

**Important:** Protected requests now have both identity and workspace context. `middleware.JWTMiddleware()` extracts `userID`; `middleware.WorkspaceMiddleware()` resolves `workspaceID` from `X-Workspace-ID` or the user's default personal workspace. Prices are scoped by `workspace_id`; most other legacy business tables still use `user_id` until later workspace ownership migrations. New workspace-aware code should use `workspaceID` and membership checks.

### Routes Structure

Routes are defined in `routes/routes.go`:
- `/api/auth/*` - Public routes for login/register (with stricter rate limiting: 10 req/min)
- `/api/*` - Protected routes requiring JWT authentication (global rate limiting: 60 req/min)

**Rate Limiting:**
- Global rate limit: 60 requests per minute per user/IP
- Authentication endpoints: 10 requests per minute per IP
- Implemented in `middleware/rate_limit.go`
- Uses in-memory storage with automatic cleanup of old timestamps
- Returns `429 Too Many Requests` with error message when exceeded

All protected routes use `middleware.JWTMiddleware()` followed by `middleware.WorkspaceMiddleware()`.

`middleware.JWTMiddleware()`:
1. Validates the JWT Bearer token
2. Checks token expiration (must not be expired)
3. Verifies signing method (HMAC only, protects against "none algorithm" attacks)
4. Validates JWT_SECRET is at least 16 characters on startup
5. Extracts `userID` from claims
6. Sets `userID` in the Gin context
7. Requires `JWT_SECRET` environment variable (min 16 characters)

`middleware.WorkspaceMiddleware()`:
1. Reads optional `X-Workspace-ID`
2. Falls back to the user's default personal workspace when the header is missing or blank
3. Rejects malformed, zero, or inaccessible workspace IDs
4. Sets `workspaceID`, `workspaceRole`, and `workspace` in the Gin context

### Controllers

Controllers in `controllers/` follow these patterns:
- Extract `userID` from context: `userID := c.MustGet("userID").(uint)`
- Extract workspace context for workspace-aware code: `workspaceID := c.MustGet("workspaceID").(uint)`
- Keep filtering current legacy business-table queries by `user_id` until those tables are migrated to `workspace_id`
- Filter price queries by `workspace_id`; keep `prices.user_id` as the creating user/audit field, not the ownership boundary
- Use GORM's `Preload()` for eager-loading relationships
- Return appropriate HTTP status codes
- Return errors as JSON: `c.JSON(statusCode, gin.H{"error": "message"})`

**Input Validation:**
- All models use `binding` tags for validation (e.g., `binding:"required,min=1"`)
- Controllers include business rules validation beyond struct tags
- Orders: quantity must be > 0, prices cannot be negative, at least 1 item required
- Products: name required, price required and non-negative, cost non-negative, package_id required
- Clients: name and surname required (min 1 character)
- All entities: name fields required (min 1 character)

### Models

Models in `models/` use GORM conventions:
- Primary key: `ID uint` with `gorm:"primaryKey"`
- Soft deletes: `DeletedAt gorm.DeletedAt` with `gorm:"index"`
- Foreign keys: Explicit `gorm:"foreignKey:Field"` tags
- Computed fields: Use `gorm:"-"` tag for fields not persisted to DB
- Validation: All required fields use `binding:"required"` and `binding:"min=X"` tags

**Key relationships:**
- User is the central entity - most models belong to a user
- Recipes have RecipeIngredients (many-to-many with ingredients)
- Recipes have CookingSessions (tracking production batches)
- Products can be associated with Recipes via ProductOptions
- Orders belong to Clients and contain OrderItems (which reference Products)

### Constants

Business constants are defined in `constants/`:
- `OrderStatus*` constants for order status values (new, in_progress, ready, finished, canceled)

### Utilities

Helper functions in `utils/`:
- `calculateIngredientCost.go` - Cost calculations
- `password.go` - Password hashing/validation
- `merge_duplicate_ingredients.go` - Deduplication logic

### Middleware

**middleware/auth.go** - JWT authentication:
- Validates JWT_SECRET on startup (min 16 characters)
- Verifies token expiration
- Checks signing method to prevent "none algorithm" attacks
- Does not reveal user existence in error messages

**middleware/rate_limit.go** - Rate limiting:
- Configurable requests per minute limit
- In-memory storage with mutex for thread safety
- Automatic cleanup of old timestamps
- Differentiates between user-based (authenticated) and IP-based (public) limiting

## API Patterns

### Authentication

Login response format:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-01-18T14:30:00Z"
}
```

**Security features:**
- JWT tokens expire after 24 hours
- Generic "Invalid credentials" error (no user enumeration)
- All timestamps in ISO 8601 format

### Order Creation/Update

Orders support automatic cost price assignment:
- If `cost_price` is provided in request items, use it
- Otherwise, fetch from `Product.Cost` field automatically
- Uses database transactions to ensure data consistency
- Replaces all order items on update (delete old, create new)
- Sorted by creation date (newest first) in list views

**Validation rules:**
- `client_id` is required
- `items` array must have at least 1 item
- Each item: `product_id` required, `quantity` > 0, `price` >= 0, `cost_price` >= 0

### Dashboard

Dashboard endpoints provide business analytics:
- `/api/dashboard` - Overview stats (recent orders, order distribution)
- `/api/dashboard/profit` - Profit data analysis

## Environment Variables

Required environment variables:
- `DATABASE_URL` - PostgreSQL connection string
- `FRONT_URL` - Frontend application URL for CORS
- `JWT_SECRET` - Secret key for JWT token signing (minimum 16 characters)

Optional (for .env file):
- Other DB configuration (currently using DATABASE_URL instead of separate DB_* vars)

**Note:** JWT_SECRET must be at least 16 characters or server will fail to start with error message.

## Conventions

1. **Data Isolation:** Legacy database queries must include `user_id` filtering; migrated tables such as prices must filter by `workspace_id`
2. **Error Handling:** Return errors as JSON with appropriate HTTP status codes
3. **Transactions:** Use `db.Begin()`, `tx.Commit()`, `tx.Rollback()` for multi-step operations
4. **Swagger:** Document all endpoints with swag comments
5. **Soft Deletes:** Models support soft deletes via GORM's `DeletedAt` field
6. **Eager Loading:** Use `Preload()` to avoid N+1 queries when returning related data
7. **Input Validation:** Always use `binding` tags on models and validate business rules in controllers
8. **Security:** Never reveal user existence in error messages, use generic responses
9. **Rate Limiting:** Public/auth endpoints have stricter limits (10/min) than general API (60/min)
10. **English Only:** **CRITICAL** - ALL code comments, documentation, variable names, function names, and text must be in English. NO Russian or other languages allowed in:
    - Code comments (`//` and `/* */` comments)
    - Documentation files (README.md, CLAUDE.md, API_DOCUMENTATION.md, etc.)
    - Variable names and function names
    - API error messages and responses
    - Swagger documentation
    - Git commit messages

## Features

### Security
- **JWT Authentication:** Token-based authentication with 24-hour expiration
- **Rate Limiting:** 60 requests/minute globally, 10 requests/minute for auth endpoints
- **Input Validation:** Comprehensive validation for all input data using struct tags
- **JWT Secret Validation:** Server validates JWT_SECRET on startup (min 16 characters)
- **Signing Method Protection:** Guards against "none algorithm" attacks
- **Token Expiration:** JWT tokens are validated for expiration on every request
- **User Enumeration Prevention:** Generic error messages for authentication failures

### Performance
- **Database Indexes:** 18 optimized indexes for fast queries
- **Auto-migration:** Automatic database schema updates on startup
- **Soft Deletes:** Data is marked as deleted, not physically removed
- **Connection Pooling:** GORM/pgx handles connection pooling automatically

### Data Integrity
- **Transaction Support:** Multi-step operations use database transactions
- **User Isolation:** All data automatically filtered by user ID
- **Validation Layers:** Both struct-level and controller-level validation
- **Business Rules:** Quantity must be positive, prices non-negative

## Recent Improvements

✅ **Security Enhancements:**
- JWT token expiration validation (24-hour tokens)
- Signing method verification in auth middleware
- JWT_SECRET validation on startup (min 16 characters)
- Secure error messages (no user enumeration)
- Rate limiting implementation (60/10 requests per minute)

✅ **Performance:**
- 18 database indexes for fast queries
- Automatic index creation on startup
- Orders sorted by creation date

✅ **Data Integrity:**
- Comprehensive input validation using binding tags
- Business rules validation in controllers
- Positive quantity validation
- Non-negative price validation

✅ **Code Quality:**
- All code comments translated to English
- Updated API documentation (API_DOCUMENTATION.md)
- Updated README.md with current features
- Enhanced CLAUDE.md development guide

## Error Response Format

All errors follow this format:
```json
{
  "error": "Error description"
}
```

Common HTTP status codes:
- `400` - Bad Request (invalid request data)
- `401` - Unauthorized (authentication required or invalid token)
- `404` - Not Found (resource not found)
- `429` - Too Many Requests (rate limit exceeded)
- `500` - Internal Server Error (server error)
