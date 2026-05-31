package database

import (
	//"fmt"
	"log"
	"os"
	// "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"mobile-backend-go/models"
)

var DB *gorm.DB
var SupportsTrigramSearch bool

// ConnectDatabase establishes database connection and runs migrations
func ConnectDatabase() {
	// dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
	//     os.Getenv("DB_USER"),
	//     os.Getenv("DB_PASSWORD"),
	//     os.Getenv("DB_HOST"),
	//     os.Getenv("DB_PORT"),
	//     os.Getenv("DB_NAME"),
	// )
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	// database, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	// if err != nil {
	//     log.Fatal("Failed to connect to database: ", err)
	// }
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database: ", err)
	}

	// Save connection to global variable
	DB = database

	if err := migratePriceQuantityToDoublePrecision(DB); err != nil {
		log.Fatal("Price quantity migration error: ", err)
	}

	// Auto-migrate all models
	err = DB.AutoMigrate(
		&models.User{},
		&models.Workspace{},
		&models.WorkspaceMember{},
		&models.Recipe{},
		&models.Ingredient{},
		&models.WorkspaceIngredient{},
		&models.RecipeIngredient{},
		&models.Price{},
		&models.CookingSession{},
		&models.CookingSessionIngredient{},
		&models.Client{},
		&models.Product{},
		&models.Package{},
		&models.ProductOption{},
		&models.Order{},
		&models.OrderItem{},
	)

	if err != nil {
		log.Fatal("Model migration error: ", err)
	}

	// Create indexes for performance
	createIndexes()

	if err := BackfillPersonalWorkspaces(DB); err != nil {
		log.Fatal("Workspace backfill error: ", err)
	}

	if err := BackfillPriceWorkspaces(DB); err != nil {
		log.Fatal("Price workspace backfill error: ", err)
	}

	if err := BackfillBusinessWorkspaces(DB); err != nil {
		log.Fatal("Business workspace backfill error: ", err)
	}

	if err := BackfillWorkspaceIngredients(DB); err != nil {
		log.Fatal("Workspace ingredient backfill error: ", err)
	}

	backfillOrderDates()

	log.Println("Migrations completed successfully.")
}

// createIndexes creates indexes for frequently queried fields
func createIndexes() {
	// Orders: frequently filtered by workspace/user, client_id, status, created_at
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_workspace_id ON orders(workspace_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_client_id ON orders(client_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at DESC)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_user_status ON orders(user_id, status)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_workspace_status ON orders(workspace_id, status)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_workspace_client_id ON orders(workspace_id, client_id)`)

	// Order Items: frequently filtered by order_id, product_id
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items(order_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_order_items_product_id ON order_items(product_id)`)

	// Products: frequently filtered by workspace/user, package_id
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_products_user_id ON products(user_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_products_workspace_id ON products(workspace_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_products_package_id ON products(package_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_products_workspace_package_id ON products(workspace_id, package_id)`)

	// Clients: frequently filtered by workspace/user
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_clients_user_id ON clients(user_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_clients_workspace_id ON clients(workspace_id)`)

	// Recipes: frequently filtered by workspace/user
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_recipes_user_id ON recipes(user_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_recipes_workspace_id ON recipes(workspace_id)`)

	// Packages: frequently filtered by workspace/user
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_packages_user_id ON packages(user_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_packages_workspace_id ON packages(workspace_id)`)

	// Ingredients: frequently filtered by name
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_ingredients_name ON ingredients(name)`)
	SupportsTrigramSearch = false
	if DB.Dialector.Name() == "postgres" {
		if err := DB.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`).Error; err != nil {
			log.Printf("pg_trgm extension is unavailable, ingredient search will use substring matching: %v", err)
		} else if err := DB.Raw(`SELECT to_regprocedure('similarity(text,text)') IS NOT NULL`).Scan(&SupportsTrigramSearch).Error; err != nil {
			log.Printf("Failed to detect pg_trgm similarity function, ingredient search will use substring matching: %v", err)
			SupportsTrigramSearch = false
		} else if SupportsTrigramSearch {
			if err := DB.Exec(`CREATE INDEX IF NOT EXISTS idx_ingredients_name_trgm ON ingredients USING gin (LOWER(name) gin_trgm_ops)`).Error; err != nil {
				log.Printf("Failed to create ingredient trigram index, fuzzy search remains enabled without the index: %v", err)
			}
		}
	}

	// Workspace Ingredients: active workspace working-set membership
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_workspace_ingredients_workspace_id ON workspace_ingredients(workspace_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_workspace_ingredients_ingredient_id ON workspace_ingredients(ingredient_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_workspace_ingredients_workspace_active ON workspace_ingredients(workspace_id, active)`)
	DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_ingredients_workspace_ingredient_active_unique ON workspace_ingredients(workspace_id, ingredient_id) WHERE deleted_at IS NULL`)

	// Workspaces: frequently resolved by personal owner and membership
	DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_personal_user_id_active_unique ON workspaces(personal_user_id) WHERE personal_user_id IS NOT NULL AND deleted_at IS NULL`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_workspace_members_user_id ON workspace_members(user_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_workspace_members_workspace_id ON workspace_members(workspace_id)`)
	DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_members_workspace_user ON workspace_members(workspace_id, user_id) WHERE deleted_at IS NULL`)

	// Prices: frequently filtered by workspace and ingredient history
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_prices_workspace_id ON prices(workspace_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_prices_workspace_ingredient_date ON prices(workspace_id, ingredient_id, date DESC)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_prices_ingredient_id ON prices(ingredient_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_prices_date ON prices(date DESC)`)

	// Cooking Sessions: frequently filtered by recipe_id, workspace/user, date
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cooking_sessions_recipe_id ON cooking_sessions(recipe_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cooking_sessions_user_id ON cooking_sessions(user_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cooking_sessions_workspace_id ON cooking_sessions(workspace_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cooking_sessions_workspace_recipe_id ON cooking_sessions(workspace_id, recipe_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_cooking_sessions_date ON cooking_sessions(date DESC)`)

	// Recipe Ingredients: frequently filtered by recipe_id, ingredient_id
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_recipe_ingredients_recipe_id ON recipe_ingredients(recipe_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_recipe_ingredients_ingredient_id ON recipe_ingredients(ingredient_id)`)

	log.Println("Indexes created successfully.")
}

func backfillOrderDates() {
	if err := DB.Exec(`UPDATE orders SET date = created_at WHERE date IS NULL`).Error; err != nil {
		log.Fatal("Order date backfill error: ", err)
	}
}
