# BatchVault Backend

## General Overview
BatchVault Backend is a REST API server written in Go that provides backend functionality for managing small food production recipes, ingredients, products, orders, clients, pricing, and dashboard analytics. The project uses a modern technology stack and follows best development practices.

## Technology Stack
- **Programming language**: Go 1.25.10
- **Web framework**: Gin
- **Database**: PostgreSQL (using GORM and pgx driver)
- **API documentation**: Swagger
- **Authentication**: JWT (24-hour token expiration)
- **Containerization**: Docker
- **Security**: Rate limiting, input validation, secure JWT handling

## Project Structure
```
.
├── controllers/       # HTTP request handlers
├── database/         # Database configuration and migrations
├── docs/            # Swagger documentation
├── middleware/      # Custom middleware (JWT, rate limiting)
├── models/          # Data models
├── routes/          # API routing
├── utils/           # Utility functions
├── constants/       # Application constants
├── main.go          # Application entry point
├── Dockerfile       # Docker configuration
└── docker-compose.yaml # Docker Compose configuration
```

## Main Dependencies
- `github.com/gin-gonic/gin` - Web framework
- `github.com/gin-contrib/cors` - CORS middleware
- `github.com/jackc/pgx/v5` - PostgreSQL driver
- `gorm.io/gorm` - ORM for database access
- `github.com/golang-jwt/jwt/v5` - JWT token handling
- `github.com/swaggo/swag` - Swagger documentation generation
- `github.com/joho/godotenv` - Environment variable loader

## Configuration
The project uses the following environment variables:
- `DATABASE_URL` - PostgreSQL connection string
- `FRONT_URL` - Frontend application URL for CORS
- `JWT_SECRET` - Secret key for JWT token signing (min 16 characters)
- `STRICT_WORKSPACE_INGREDIENTS` - Optional feature flag. Set to `true` to require active workspace ingredient membership for new price and recipe-ingredient writes. Defaults to compatibility mode when unset.
- `WORKSPACE_CURRENCY_UPDATES_ENABLED` - Optional rollout gate. Set to `true` to enable `PATCH /api/workspaces/current`; the endpoint is disabled by default while currency reads remain available.

Environment variables can be defined:
1. Directly in the system
2. In a `.env` file (which must not be committed to version control)

## API Documentation
API documentation is available via Swagger UI at: `http://localhost:8080/swagger/index.html`

## Container Image
The backend image is published by GitHub Actions to:

- `ghcr.io/dzarlax/batchvault-back:latest`
- `ghcr.io/dzarlax/batchvault-back:<commit-sha>`

## Features

### 🔐 Security
- **JWT Authentication**: Token-based authentication with 24-hour expiration
- **Rate Limiting**: 60 requests/minute globally, 10 requests/minute for auth endpoints
- **Input Validation**: Comprehensive validation for all input data
- **JWT Secret Validation**: Server validates JWT_SECRET on startup (min 16 characters)
- **Signing Method Protection**: Guards against "none algorithm" attacks

### 🚀 Performance
- **Database Indexes**: Optimized indexes for fast user, workspace, and operational queries
- **Auto-migration**: Automatic database schema updates on startup
- **Soft Deletes**: Data is marked as deleted, not physically removed

### ✅ Data Integrity
- **Input Validation**: All requests validated before processing
- **Transaction Support**: Multi-step operations use database transactions
- **Scoped Access**: Legacy business data is filtered by user ID; workspace context is resolved for protected requests as the foundation for workspace ownership
- **Order Dates**: Orders expose a business `date`; existing rows are backfilled from `created_at`, and new orders fall back to the creation time when no date is provided

## API Endpoints

### Authentication
- `POST /api/auth/register` - Register new user
- `POST /api/auth/login` - Authenticate and receive JWT token

### Workspaces
- `GET /api/workspaces` - List workspaces available to the authenticated user
- `GET /api/workspaces/current` - Get the workspace resolved for the current request
- `GET /api/currencies` - List supported monetary ISO 4217 currencies
- `PATCH /api/workspaces/current` - Update the current workspace currency when `WORKSPACE_CURRENCY_UPDATES_ENABLED=true` and the requester is a workspace owner

### Recipes
- `GET /api/recipes` - Get all recipes
- `GET /api/recipes/:id` - Get recipe by ID
- `POST /api/recipes` - Create new recipe
- `DELETE /api/recipes/:id` - Delete recipe

### Ingredients
- `GET /api/ingredients` - Get all ingredients
- `GET /api/ingredients/check` - Check if ingredient exists by name
- `POST /api/ingredients` - Create new ingredient

### Recipe Ingredients
- `POST /api/recipes/:id/ingredients` - Add ingredient to recipe
- `DELETE /api/recipes/:id/ingredients/:ingredient_id` - Remove ingredient from recipe

### Products
- `GET /api/products` - Get all products
- `GET /api/products/:id` - Get product by ID
- `POST /api/products` - Create new product
- `PUT /api/products/:id` - Update product
- `DELETE /api/products/:id` - Delete product

### Prices
- `GET /api/prices` - Get ingredient prices
- `POST /api/prices` - Add new price for ingredient

### Dashboard
- `GET /api/dashboard` - Get dashboard statistics
- `GET /api/dashboard/profit` - Get profit analysis

### Clients
- `GET /api/clients` - Get all clients
- `GET /api/clients/:id` - Get client by ID
- `POST /api/clients` - Create new client
- `PUT /api/clients/:id` - Update client
- `DELETE /api/clients/:id` - Delete client

### Orders
- `GET /api/orders` - Get all orders (sorted by creation date)
- `GET /api/orders/:id` - Get order by ID
- `POST /api/orders` - Create new order
- `PUT /api/orders/:id` - Update order
- `PUT /api/orders/:id/status` - Update order status
- `DELETE /api/orders/:id` - Delete order

### Packages
- `GET /api/packages` - Get all packages
- `POST /api/packages` - Create new package

### Profile
- `POST /api/profile/change-password` - Change user password

## Authentication

All API endpoints except `/api/auth/register` and `/api/auth/login` require authentication using a JWT Bearer token.

**Header format:** `Authorization: Bearer <your_token>`

Protected routes also resolve workspace context. Clients may send `X-Workspace-ID: <id>` to select a workspace. If the header is omitted or blank, the backend uses the user's default personal workspace. Prices are scoped by `workspace_id`; most other legacy business records are still scoped by `user_id` until later migrations.

**Login Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-01-18T14:30:00Z"
}
```

**Token expiration:** 24 hours

## Rate Limiting

The API implements rate limiting to prevent abuse:

- **Global limit:** 60 requests per minute per user/IP
- **Authentication endpoints:** 10 requests per minute per IP
- **Headers:** `X-RateLimit-Limit` included in responses

**Rate Limit Response (429 Too Many Requests):**
```json
{
  "error": "Rate limit exceeded: maximum 60 requests per minute"
}
```

## Error Responses

All errors are returned in the following format:

```json
{
  "error": "Error description"
}
```

### Common Error Codes:
- `400` - Bad Request (invalid request data)
- `401` - Unauthorized (authentication required or invalid token)
- `404` - Not Found (resource not found)
- `429` - Too Many Requests (rate limit exceeded)
- `500` - Internal Server Error (server error)

## Input Validation

The API validates all input data:

**Orders:**
- `quantity` must be greater than 0
- `price` cannot be negative
- `cost_price` cannot be negative
- At least one item is required

**Products:**
- `name` is required (min 1 character)
- `price` is required and cannot be negative
- `cost` cannot be negative
- `package_id` is required

**Clients:**
- `name` is required (min 1 character)
- `surname` is required (min 1 character)

**Recipes, Ingredients, Packages:**
- `name` is required (min 1 character)

## Running the Project

### Local Run
1. Install Go 1.23 or higher
2. Create a `.env` file with the required environment variables
3. Run the application: `go run main.go`

### Running with Docker
1. Build and run with Docker Compose: `docker-compose up --build`
2. View logs: `docker-compose logs -f`

## Development

### Code Quality
1. Code must follow Go standards
2. All comments and documentation must be in English
3. All new endpoints must be documented via Swagger
4. Database changes must use GORM auto-migration
5. Existing legacy business data must be filtered by `user_id` until the table is migrated to `workspace_id`; prices are already workspace-scoped
6. New workspace-aware code must validate workspace membership and use `workspaceID` from request context

### Generating Swagger Documentation
```bash
swag init -g main.go
```

## Security Best Practices

1. **Always validate input data** - Use binding tags and custom validation
2. **Filter by owner scope** - Legacy business queries still filter by `user_id`; workspace-aware queries, including prices, must filter by `workspace_id` after membership validation
3. **Use transactions** - For multi-step database operations
4. **Never log sensitive data** - Don't log passwords, tokens, or personal info
5. **Validate JWT_SECRET** - Minimum 16 characters for security
6. **Handle errors gracefully** - Return generic error messages to clients

## Database Optimization

The application automatically creates indexes on startup for optimal query performance:
- Workspaces: `personal_user_id`, membership lookup, unique active membership
- Prices: `workspace_id`, `workspace_id + ingredient_id + date`, plus legacy ingredient/date indexes
- Orders: `user_id`, `client_id`, `status`, `created_at`, composite indexes
- Products: `user_id`, `package_id`
- And more...

Indexes are created automatically - no manual setup required.

## Limitations

- The server runs on port 8080
- JWT tokens expire after 24 hours
- Rate limiting is in-memory (resets on server restart)
- Soft delete is enabled on all models

## Recent Improvements

✅ **Security Enhancements:**
- JWT token expiration validation
- Signing method verification
- JWT_SECRET validation on startup
- Secure error messages (no user enumeration)

✅ **Performance:**
- 18 database indexes for fast queries
- Automatic index creation on startup

✅ **Data Integrity:**
- Comprehensive input validation
- Transaction support for multi-step operations
- Positive quantity validation
- Non-negative price validation

✅ **API Protection:**
- Rate limiting (60/10 requests per minute)
- User-based and IP-based limits
- Automatic cleanup of old rate limit data

✅ **Documentation:**
- All code comments in English
- Updated API documentation
- Swagger documentation

## License

[Add your license information here]
