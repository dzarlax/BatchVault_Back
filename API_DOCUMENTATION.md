# BatchVault Backend API Documentation

## Overview

BatchVault Backend API is a RESTful API for managing recipes, ingredients, products, orders, clients, pricing, and dashboard workflows for small food production.

**Base URL:** `http://localhost:8080`

**Swagger UI:** `http://localhost:8080/swagger/index.html`

## Authentication

The API uses JWT (JSON Web Token) for authentication. After successful login, you will receive a token that must be included in the `Authorization` header for all protected endpoints.

**Header format:** `Authorization: Bearer <your_jwt_token>`

Protected routes also resolve workspace context. Clients may send `X-Workspace-ID: <workspace_id>`. Missing or blank `X-Workspace-ID` falls back to the user's default personal workspace. Malformed, zero, or inaccessible workspace IDs are rejected.

Prices are scoped by the resolved workspace. `user_id` on price responses remains the creating user/audit field during the workspace migration.

**Token expiration:** 24 hours

Orders include a business `date` field. When clients omit it on create, the backend falls back to the order creation time. Existing orders are backfilled from `created_at`.

---

## API Endpoints

### 🔐 Authentication

### Workspaces

#### GET `/api/workspaces`
Returns workspaces available to the authenticated user.

**Headers:**
- `Authorization: Bearer <your_jwt_token>`

**Response:**
```json
[
  {
    "id": 1,
    "name": "Personal workspace",
    "slug": "personal-1",
    "role": "owner"
  }
]
```

#### GET `/api/workspaces/current`
Returns the workspace resolved for the current request.

**Headers:**
- `Authorization: Bearer <your_jwt_token>`
- `X-Workspace-ID: 1` optional

**Response:**
```json
{
  "id": 1,
  "name": "Personal workspace",
  "slug": "personal-1",
  "role": "owner"
}
```

#### POST `/api/auth/register`
Регистрация нового пользователя.

**Request Body:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response (201):**
```json
{
  "id": 1,
  "username": "test_user",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Errors:**
- `400` - Пользователь уже существует
- `400` - Неверные данные запроса

---

#### POST `/api/auth/login`
Authenticate user.

**Request Body:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response (200):**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-01-18T14:30:00Z"
}
```

**Errors:**
- `401` - Invalid credentials
- `400` - Invalid request data
- `429` - Rate limit exceeded (max 10 requests per minute for auth endpoints)

---

### 🍳 Recipes

#### GET `/api/recipes`
Получение списка рецептов пользователя.

**Query Parameters:**
- `recipe_id` (optional) - Фильтрация по ID рецепта
- `ingredient_id` (optional) - Фильтрация по ID ингредиента

**Response (200):**
```json
[
  {
    "id": 1,
    "name": "Beef Jerky Original",
    "user_id": 1,
    "total_cost": 250.50,
    "recipe_ingredients": [
      {
        "id": 1,
        "recipe_id": 1,
        "ingredient_id": 1,
        "quantity": 1000,
        "unit": "g",
        "calculated_cost": 180.00,
        "ingredient": {
          "id": 1,
          "name": "Говядина",
          "type": "Мясо",
          "prices": [
            {
              "id": 1,
              "price": 450.00,
              "quantity": 1,
              "unit": "kg",
              "date": "2024-01-01T00:00:00Z"
            }
          ]
        }
      }
    ],
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

#### GET `/api/recipes/{id}`
Получение рецепта по ID.

**Path Parameters:**
- `id` - ID рецепта

**Response (200):** Аналогично GET `/api/recipes`, но один объект

**Errors:**
- `404` - Рецепт не найден
- `400` - Неверный ID рецепта

---

#### POST `/api/recipes`
Создание нового рецепта.

**Request Body:**
```json
{
  "name": "New Recipe Name"
}
```

**Response (201):**
```json
{
  "id": 2,
  "name": "New Recipe Name",
  "user_id": 1,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

#### DELETE `/api/recipes/{id}`
Удаление рецепта.

**Path Parameters:**
- `id` - ID рецепта

**Response (200):**
```json
{
  "message": "Recipe deleted successfully"
}
```

**Errors:**
- `404` - Рецепт не найден

---

### 🥗 Ingredients

#### GET `/api/ingredients`
Получение списка ингредиентов.

**Response (200):**
```json
[
  {
    "id": 1,
    "name": "Говядина",
    "type": "Мясо",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

#### POST `/api/ingredients`
Создание нового ингредиента.

**Request Body:**
```json
{
  "name": "Новый ингредиент",
  "type": "Тип ингредиента"
}
```

**Response (201):**
```json
{
  "id": 2,
  "name": "Новый ингредиент",
  "type": "Тип ингредиента",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Errors:**
- `400` - Ингредиент с таким именем уже существует

---

#### GET `/api/ingredients/check`
Проверка существования ингредиента по имени.

**Query Parameters:**
- `name` (required) - Имя ингредиента для проверки

**Response (200):**
```json
{
  "exists": true,
  "ingredient": {
    "id": 1,
    "name": "Говядина",
    "type": "Мясо"
  }
}
```

или

```json
{
  "exists": false
}
```

---

### 🔗 Recipe Ingredients

#### POST `/api/recipes/{id}/ingredients`
Добавление ингредиента к рецепту.

**Path Parameters:**
- `id` - ID рецепта

**Request Body:**
```json
{
  "ingredient_id": 1,
  "quantity": 1000,
  "unit": "g"
}
```

**Response (201):**
```json
{
  "id": 1,
  "recipe_id": 1,
  "ingredient_id": 1,
  "quantity": 1000,
  "unit": "g",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

#### DELETE `/api/recipes/{id}/ingredients/{ingredient_id}`
Удаление ингредиента из рецепта.

**Path Parameters:**
- `id` - ID рецепта
- `ingredient_id` - ID ингредиента

**Response (200):**
```json
{
  "message": "Ingredient removed from recipe successfully"
}
```

---

### 📦 Products

#### GET `/api/products`
Получение списка продуктов пользователя.

**Response (200):**
```json
[
  {
    "id": 1,
    "name": "Beef Jerky Original",
    "description": "Classic beef jerky",
    "price": 300.00,
    "cost": 180.00,
    "image": "beef_jerky.jpg",
    "package_id": 1,
    "user_id": 1,
    "package": {
      "id": 1,
      "name": "Стандартная упаковка",
      "user_id": 1,
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    },
    "options": [
      {
        "id": 1,
        "product_id": 1,
        "recipe_id": 1,
        "user_id": 1,
        "created_at": "2024-01-01T00:00:00Z"
      }
    ],
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

#### GET `/api/products/{id}`
Получение продукта по ID.

**Path Parameters:**
- `id` - ID продукта

**Response (200):** Аналогично GET `/api/products`, но один объект

**Errors:**
- `404` - Продукт не найден
- `400` - Неверный ID продукта

---

#### POST `/api/products`
Создание нового продукта.

**Request Body:**
```json
{
  "name": "New Product",
  "description": "Product description"
}
```

**Response (201):**
```json
{
  "id": 2,
  "name": "New Product",
  "description": "Product description",
  "user_id": 1,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

#### PUT `/api/products/{id}`
Обновление продукта.

**Path Parameters:**
- `id` - ID продукта

**Request Body:**
```json
{
  "name": "Updated Product",
  "description": "Updated description"
}
```

**Response (200):**
```json
{
  "id": 1,
  "name": "Updated Product",
  "description": "Updated description",
  "user_id": 1,
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

#### DELETE `/api/products/{id}`
Удаление продукта.

**Path Parameters:**
- `id` - ID продукта

**Response (200):**
```json
{
  "message": "Product deleted successfully"
}
```

---

### 💰 Prices

#### GET `/api/prices`
Получение списка цен на ингредиенты.

Returns prices for the resolved workspace. Use `X-Workspace-ID` to select a workspace, or omit it to use the default personal workspace.
Price `quantity` is a JSON number and may be decimal, for example `2.5`.

**Query Parameters:**
- `ingredient_id` (optional) - Фильтрация по ID ингредиента

**Response (200):**
```json
[
  {
    "id": 1,
    "ingredient_id": 1,
    "price": 450.00,
    "quantity": 2.5,
    "unit": "kg",
    "date": "2024-01-01T00:00:00Z",
    "user_id": 1,
    "workspace_id": 1,
    "ingredient": {
      "id": 1,
      "name": "Говядина",
      "type": "Мясо"
    }
  }
]
```

---

#### POST `/api/prices`
Добавление новой цены для ингредиента.

Creates a price in the resolved workspace. The request body does not accept `workspace_id`; it is taken from `X-Workspace-ID` or the default personal workspace.
Price `quantity` must be greater than zero and may be decimal.

**Request Body:**
```json
{
  "ingredient_id": 1,
  "price": 500.00,
  "quantity": 2.5,
  "unit": "kg",
  "date": "2024-01-01T00:00:00Z"
}
```

**Response (201):**
```json
{
  "id": 2,
  "ingredient_id": 1,
  "price": 500.00,
  "quantity": 2.5,
  "unit": "kg",
  "date": "2024-01-01T00:00:00Z",
  "user_id": 1,
  "workspace_id": 1
}
```

---

### 👥 Clients

#### GET `/api/clients`
Получение списка клиентов.

**Response (200):**
```json
[
  {
    "id": 1,
    "name": "Иван Иванов",
    "email": "ivan@example.com",
    "phone": "+7-999-123-45-67",
    "address": "Москва, ул. Примерная, 1",
    "user_id": 1,
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

#### GET `/api/clients/{id}`
Получение клиента по ID.

**Path Parameters:**
- `id` - ID клиента

**Response (200):** Аналогично GET `/api/clients`, но один объект

---

#### POST `/api/clients`
Создание нового клиента.

**Request Body:**
```json
{
  "name": "Новый Клиент",
  "email": "client@example.com",
  "phone": "+7-999-000-00-00",
  "address": "Адрес клиента"
}
```

**Response (201):**
```json
{
  "id": 2,
  "name": "Новый Клиент",
  "email": "client@example.com",
  "phone": "+7-999-000-00-00",
  "address": "Адрес клиента",
  "user_id": 1,
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

#### PUT `/api/clients/{id}`
Обновление клиента.

**Path Parameters:**
- `id` - ID клиента

**Request Body:**
```json
{
  "name": "Обновленное имя",
  "email": "updated@example.com",
  "phone": "+7-999-111-11-11",
  "address": "Новый адрес"
}
```

**Response (200):** Обновленный объект клиента

---

#### DELETE `/api/clients/{id}`
Удаление клиента.

**Path Parameters:**
- `id` - ID клиента

**Response (200):**
```json
{
  "message": "Client deleted successfully"
}
```

---

### 📋 Orders

#### GET `/api/orders`
Получение списка заказов. Заказы отсортированы по дате создания (новые сверху).

**Response (200):**
```json
[
  {
    "id": 1,
    "client_id": 1,
    "status": "new",
    "comment": "Особые пожелания клиента по доставке",
    "user_id": 1,
    "client": {
      "id": 1,
      "name": "Иван Иванов",
      "email": "ivan@example.com",
      "phone": "+7-999-123-45-67",
      "address": "Москва, ул. Примерная, 1"
    },
    "items": [
      {
        "id": 1,
        "order_id": 1,
        "product_id": 1,
        "quantity": 5,
        "price": 300.00,
        "cost_price": 180.00,
        "product": {
          "id": 1,
          "name": "Beef Jerky Original",
          "description": "Classic beef jerky",
          "price": 300.00,
          "cost": 180.00,
          "package": {
            "id": 1,
            "name": "Стандартная упаковка"
          }
        }
      }
    ],
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

#### GET `/api/orders/{id}`
Получение заказа по ID.

**Path Parameters:**
- `id` - ID заказа

**Response (200):** Аналогично GET `/api/orders`, но один объект

---

#### POST `/api/orders`
Создание нового заказа.

**Request Body:**
```json
{
  "client_id": 1,
  "status": "new",
  "comment": "Особые пожелания клиента по доставке",
  "items": [
    {
      "product_id": 1,
      "quantity": 5,
      "price": 300.00,
      "cost_price": 180.00
    },
    {
      "product_id": 2,
      "quantity": 3,
      "price": 250.00
    }
  ]
}
```

**Примечания:**
- `cost_price` - необязательное поле. Если не указано, автоматически используется себестоимость из продукта
- `status` - необязательное поле. По умолчанию устанавливается "new"
- `comment` - необязательное поле. Комментарий к заказу
- Возможные статусы заказа: "new", "in_progress", "ready", "finished", "canceled"

**Response (201):**
```json
{
  "id": 1,
  "client_id": 1,
  "status": "new",
  "comment": "Особые пожелания клиента по доставке",
  "user_id": 1,
  "client": {
    "id": 1,
    "name": "Иван Иванов",
    "email": "ivan@example.com",
    "phone": "+7-999-123-45-67",
    "address": "Москва, ул. Примерная, 1"
  },
  "items": [
    {
      "id": 1,
      "order_id": 1,
      "product_id": 1,
      "quantity": 5,
      "price": 300.00,
      "cost_price": 180.00,
      "product": {
        "id": 1,
        "name": "Beef Jerky Original",
        "description": "Classic beef jerky",
        "price": 300.00,
        "cost": 180.00,
        "package": {
          "id": 1,
          "name": "Стандартная упаковка"
        }
      }
    },
    {
      "id": 2,
      "order_id": 1,
      "product_id": 2,
      "quantity": 3,
      "price": 250.00,
      "cost_price": 150.00,
      "product": {
        "id": 2,
        "name": "Beef Jerky Spicy",
        "description": "Spicy beef jerky",
        "price": 250.00,
        "cost": 150.00
      }
    }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Errors:**
- `400` - Неверные данные запроса
- `400` - Клиент не найден или не принадлежит пользователю
- `400` - Продукт не найден или не принадлежит пользователю

---

#### PUT `/api/orders/{id}`
Обновление заказа.

**Path Parameters:**
- `id` - ID заказа

**Request Body:** Аналогично POST `/api/orders` (включая поле `comment`)

**Response (200):** Обновленный объект заказа

---

#### PUT `/api/orders/{id}/status`
Обновление статуса заказа.

**Path Parameters:**
- `id` - ID заказа

**Request Body:**
```json
{
  "status": "ready"
}
```

**Response (200):**
```json
{
  "message": "Order status updated successfully"
}
```

---

#### DELETE `/api/orders/{id}`
Удаление заказа.

**Path Parameters:**
- `id` - ID заказа

**Response (200):**
```json
{
  "message": "Order deleted successfully"
}
```

---

### 📦 Packages

#### GET `/api/packages`
Получение списка упаковок.

**Response (200):**
```json
[
  {
    "id": 1,
    "name": "Стандартная упаковка",
    "weight": 100,
    "price": 50.00,
    "user_id": 1,
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

---

#### POST `/api/packages`
Создание новой упаковки.

**Request Body:**
```json
{
  "name": "Новая упаковка",
  "weight": 150,
  "price": 75.00
}
```

**Response (201):** Созданный объект упаковки

---

### 📊 Dashboard

#### GET `/api/dashboard`
Получение данных для дашборда.

**Response (200):**
```json
{
  "total_recipes": 15,
  "total_products": 8,
  "total_orders": 42,
  "pending_orders": 5,
  "recent_orders": [
    {
      "id": 1,
      "client_name": "Иван Иванов",
      "total_amount": 1500.00,
      "status": "new",
      "order_date": "2024-01-01T00:00:00Z"
    }
  ],
  "order_type_distribution": [
    {
      "type": "new",
      "count": 3
    },
    {
      "type": "in_progress",
      "count": 2
    },
    {
      "type": "ready",
      "count": 1
    },
    {
      "type": "finished",
      "count": 35
    },
    {
      "type": "canceled",
      "count": 1
    }
  ]
}
```

**Описание полей:**
- `total_recipes` - общее количество рецептов пользователя
- `total_products` - общее количество продуктов пользователя
- `total_orders` - общее количество заказов пользователя
- `pending_orders` - количество незавершенных заказов (не "finished" и не "canceled")
- `recent_orders` - список последних 5 заказов с рассчитанной суммой
- `order_type_distribution` - распределение заказов по статусам

---

### 👤 Profile

#### POST `/api/profile/change-password`
Изменение пароля пользователя.

**Request Body:**
```json
{
  "current_password": "current_password",
  "new_password": "new_password"
}
```

**Response (200):**
```json
{
  "message": "Password changed successfully"
}
```

**Errors:**
- `400` - Неверный текущий пароль
- `400` - Неверные данные запроса

---

## Error Responses

All errors are returned in the following format:

```json
{
  "error": "Error description"
}
```

### Common Error Codes:
- `400` - Bad Request (invalid request data)
- `401` - Unauthorized (authentication required)
- `403` - Forbidden (insufficient permissions)
- `404` - Not Found (resource not found)
- `429` - Too Many Requests (rate limit exceeded)
- `500` - Internal Server Error (server error)

---

## Rate Limiting

The API implements rate limiting to prevent abuse:

- **Global limit:** 60 requests per minute per user/IP
- **Authentication endpoints:** 10 requests per minute per IP
- **Headers:** Responses include `X-RateLimit-Limit` header

**Rate Limit Response (429):**
```json
{
  "error": "Rate limit exceeded: maximum 60 requests per minute"
}
```

---

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

---

## Data Models

### User
```json
{
  "id": "uint",
  "username": "string",
  "password": "string (hashed)",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Recipe
```json
{
  "id": "uint",
  "name": "string",
  "user_id": "uint",
  "total_cost": "float64 (calculated)",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Ingredient
```json
{
  "id": "uint",
  "name": "string (unique)",
  "type": "string",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Price
```json
{
  "id": "uint",
  "ingredient_id": "uint",
  "price": "float64",
  "quantity": "float64",
  "unit": "string",
  "date": "timestamp",
  "user_id": "uint"
}
```

### Client
```json
{
  "id": "uint",
  "name": "string",
  "email": "string",
  "phone": "string",
  "address": "string",
  "user_id": "uint",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Product
```json
{
  "id": "uint",
  "name": "string",
  "description": "string",
  "price": "float64",
  "cost": "float64",
  "image": "string",
  "package_id": "uint",
  "user_id": "uint",
  "package": "Package object",
  "options": "ProductOption array",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Package
```json
{
  "id": "uint",
  "name": "string",
  "user_id": "uint",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### ProductOption
```json
{
  "id": "uint",
  "product_id": "uint",
  "recipe_id": "uint",
  "user_id": "uint",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Order
```json
{
  "id": "uint",
  "client_id": "uint",
  "status": "string (new|in_progress|ready|finished|canceled)",
  "comment": "string",
  "total_amount": "float64",
  "order_date": "timestamp",
  "delivery_date": "timestamp",
  "user_id": "uint",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

#### GET `/api/dashboard/profit`
Получение данных о прибыли.

**Response (200):**
```json
{
  "total_revenue": 15000.00,
  "total_costs": 9000.00,
  "total_profit": 6000.00,
  "order_count": 25
}
```

**Описание полей:**
- `total_revenue` - общая выручка от завершенных заказов
- `total_costs` - общая себестоимость завершенных заказов  
- `total_profit` - чистая прибыль (выручка - себестоимость)
- `order_count` - количество завершенных заказов

**Notes:**
- Calculation is performed only for orders with status "finished"
- Revenue: `SUM(price * quantity)` for all items in finished orders
- Cost: `SUM(cost_price * quantity)` for all items in finished orders

---

## Notes

1. **Authentication**: All endpoints except `/api/auth/*` require JWT token
2. **User Filtering**: All data is automatically filtered by current user ID
3. **Cost Calculation**: Recipe costs are calculated automatically based on latest ingredient prices
4. **Ingredient Uniqueness**: System prevents creation of duplicate ingredients
5. **Soft Delete**: Models use soft delete (records are marked as deleted but not physically removed)
6. **Rate Limiting**: API enforces rate limits to prevent abuse (60 req/min globally, 10 req/min for auth)
7. **Input Validation**: All inputs are validated for data integrity (positive quantities, non-negative prices, required fields)
8. **JWT Expiration**: Tokens expire after 24 hours for enhanced security
9. **Index Optimization**: Database indexes are automatically created for optimal query performance 

## Product cost calculation

All costing reads and writes use the resolved workspace. Existing products stay in `manual` mode; legacy `cost` values and saved order costs are preserved. The new metadata is additive. Updated clients must use `cost_estimate` for calculated products and must never treat a legacy `cost` value as a fallback for an incomplete calculation.

### Recipe and package inputs

- `POST /api/recipes` accepts optional `planned_yield_g`; `PATCH /api/recipes/{id}` updates only this field. It is the finished content weight for the recipe's listed ingredient quantities, after processing. A positive decimal with at most three fractional digits is required; `null` clears it, omission preserves it on PATCH.
- `POST /api/packages` accepts optional `net_weight_g` and `unit_cost`; `PATCH /api/packages/{id}` changes these fields and/or `name`. Net weight is positive with at most three fractional digits. Unit cost is nonnegative with at most two fractional digits. `null` means unknown; an explicit zero unit cost means free packaging. Names are never parsed for weight or cost.
- First-time initialization of a used package's unknown weight requires `confirm_weight_initialization: true` after reviewing linked products. An existing assortment that does not match the proposed weight returns 409. Once a used package has a weight, create another format to change it. Unused formats remain editable. Changing unit cost affects current estimates, never saved order costs.
- Ingredient price creation requires an explicit non-null `price`, including zero, and a finite positive purchased `quantity`. Latest prices are selected within the workspace by date, creation timestamp and ID, descending.

### Product configuration and preview

`POST /api/products` and `PUT /api/products/{id}` accept:

```json
{
  "name": "Five flavours, 250 g",
  "price": 1200,
  "package_id": 12,
  "cost_mode": "assortment",
  "cost_recipe_id": null,
  "components": [
    {"recipe_id": 1, "quantity_g": 50},
    {"recipe_id": 2, "quantity_g": 50},
    {"recipe_id": 3, "quantity_g": 50},
    {"recipe_id": 4, "quantity_g": 50},
    {"recipe_id": 5, "quantity_g": 50}
  ]
}
```

`manual` uses the complete legacy `cost`, with no additional packaging. `recipe` requires exactly one `cost_recipe_id` and no components. `assortment` requires at least two distinct component recipes and positive gram amounts, with no `cost_recipe_id`. Component grams must equal the known package net weight exactly. Existing `recipe_ids` links remain independent and never select a costing mode automatically. Omitted costing configuration is preserved on update. Entering `cost` on a calculated product returns 409 unless the request explicitly switches to `manual`.

`POST /api/products/cost-preview` accepts the same costing fields plus `package_id` (and `cost` for manual preview), without a name/price. It returns a `CostEstimate` and writes no rows or notifications. Malformed composition or foreign references return 400. Structurally valid configurations with missing yield/price/package metadata can be saved and return an incomplete estimate.

The ordinary formula is `ingredient batch cost / planned_yield_g * package net_weight_g + package unit_cost`. Assortments sum each recipe's per-gram rate times its assigned grams, then add exactly one final package. There are no inner packages, alternative recipes or nested sets. Recognized `packaging` ingredients in a costing recipe make it incomplete so packaging is not charged twice. They remain in the legacy ingredient subtotal.

Calculations use base-ten rational arithmetic, preserve unrounded component amounts internally and round the final sale-unit total once, to two fractional digits, half away from zero. New weight columns are `numeric(14,3)`, packaging cost is `numeric(14,2)`; calculated totals are bounded to the latter monetary range. Other legacy money columns are unchanged.

Product list/detail responses include `cost_estimate` with `status`, `source`, nullable `total_cost`, `content_cost`, `packaging_cost`, recipe components, input price references, issues and a calculation timestamp. `complete` with `total_cost: 0` is valid. `incomplete` has `total_cost: null`; clients show issues and suppress profit/margin. Recipe responses also include planned yield, per-gram cost and ingredient `cost_status`; legacy `total_cost` remains a potentially partial ingredient subtotal. These estimates cover ingredients and packaging only.

### Orders and historical costs

For every new/replaced calculated order item the server recomputes the current estimate in the write transaction. Updated clients submit their displayed two-decimal `cost_price` as an assertion. A different rounded amount or incomplete calculation returns 409 and rolls back the entire write. Clients refresh and explicitly review the new amount. Legacy omission accepts the current cost at save time. Manual items permit an explicit nonnegative cost override, including zero.

The server persists `cost_price` and a versioned JSON `cost_snapshot` containing source, amount, estimate/input references and timestamp. Clients send stable order item `id` values on PUT. Unchanged saved rows retain their cost and snapshot across quantity/status/price edits, product source changes and product deletion. Calculated historical cost cannot be overridden. Legacy rows with no snapshot remain historical manual rows; explicit edits create manual provenance. Unique ID-less legacy matches remain supported, but ambiguous calculated matches return 409. Unknown snapshot versions/sources are rejected on edit.

POST returns the saved order. PUT retains its success-message response; clients then GET `/api/orders/{id}` to obtain authoritative saved rows/snapshots. After a successful PUT, a failed GET is recovered by retrying only the read, never the write. Historical order totals and profit always use saved item costs, never live estimates.

### Local verification

Focused costing suites run with `go test ./controllers ./services`. The PostgreSQL migration/JSONB/decimal test is opt-in with `COSTING_TEST_DATABASE_URL` and accepts only a local database named `costing_test`; it uses and drops its own unique schema. No production data is required. Schema changes are additive and migration can run repeatedly. Rollback requires retaining these fields and snapshots; do not drop them or operate calculated products through older clients that ignore `cost_mode`.


### Package concurrency and costing regression tests

Package PATCH uses PostgreSQL READ COMMITTED while holding the same package row lock acquired by product writers. Its usage/composition checks run after acquiring that lock and therefore see newly committed product links. Product/order costing and previews retain repeatable-read graph consistency. An established weight change, unreviewed initialization or mismatching assortment must return 409 even when a product is committed while PATCH waits.

The opt-in controller test `TestPackageMutationObservesProductCommittedWhileWaiting` uses actual concurrent handlers, an insert barrier and an observed PostgreSQL lock wait. Set `COSTING_TEST_DATABASE_URL` to an isolated local database named `costing_test` and run `go test ./controllers ./database -run 'PackageMutation|ProductCostingPostgres' -count=1`. The fixtures create and drop unique schemas; no production database is accepted.

Web `npm run test:costing` checks the shared network quote-refresh operation. `npm run test:costing:browser` is opt-in against synthetic localhost fixtures and verifies both order surfaces through 409, refresh and save. iOS `OrderCostReviewTests` covers quote fetches, historical selection restoration, price retention and late-response rejection. Android `OrdersViewModelRecoveryTest` covers per-order pending reads and editor navigation after successful PUT/failed GET.
