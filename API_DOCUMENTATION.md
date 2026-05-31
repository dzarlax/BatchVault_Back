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
