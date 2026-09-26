package routes

import (
	"mobile-backend-go/controllers"
	"mobile-backend-go/middleware"

	"github.com/gin-gonic/gin"
)

// SetupRoutes initializes API routes
func SetupRoutes(router *gin.Engine) {
	// Global rate limiting: 60 requests per minute
	router.Use(middleware.RateLimitMiddleware(60))

	// Authentication routes (with stricter limit)
	authRoutes := router.Group("/api/auth")
	authRoutes.Use(middleware.RateLimitMiddleware(10)) // 10 requests per minute for auth
	{
		authRoutes.POST("/register", controllers.Register)
		authRoutes.POST("/login", controllers.Login)
	}

	// Protected routes group
	protectedRoutes := router.Group("/api")
	protectedRoutes.Use(middleware.JWTMiddleware())
	{
		// Workspace routes
		protectedRoutes.GET("/workspaces", controllers.GetWorkspaces)
		protectedRoutes.GET("/currencies", controllers.GetCurrencies)
		protectedRoutes.PUT("/push-devices/current", controllers.UpsertCurrentPushDevice)
		protectedRoutes.DELETE("/push-devices/current", controllers.DisableCurrentPushDevice)

		protectedRoutes.Use(middleware.WorkspaceMiddleware())
		protectedRoutes.GET("/workspaces/current", controllers.GetCurrentWorkspace)
		if controllers.WorkspaceCurrencyUpdatesEnabled() {
			protectedRoutes.PATCH("/workspaces/current", controllers.UpdateCurrentWorkspaceCurrency)
		}

		// Recipe routes
		protectedRoutes.GET("/recipes", controllers.GetRecipes)
		protectedRoutes.GET("/recipes/:id", controllers.GetRecipe)
		protectedRoutes.POST("/recipes", controllers.CreateRecipe)
		protectedRoutes.DELETE("/recipes/:id", controllers.DeleteRecipe)

		// Ingredient routes
		protectedRoutes.POST("/ingredients", controllers.CreateIngredient)
		protectedRoutes.GET("/ingredients", controllers.GetIngredients)
		protectedRoutes.GET("/ingredients/search", controllers.SearchIngredients)
		protectedRoutes.GET("/ingredients/check", controllers.CheckIngredientExists)
		protectedRoutes.GET("/workspace-ingredients", controllers.GetWorkspaceIngredients)
		protectedRoutes.POST("/workspace-ingredients", controllers.AddWorkspaceIngredient)
		protectedRoutes.PATCH("/workspace-ingredients/:id", controllers.UpdateWorkspaceIngredient)
		protectedRoutes.DELETE("/workspace-ingredients/:id", controllers.DeleteWorkspaceIngredient)

		// Recipe ingredient routes
		protectedRoutes.POST("/recipes/:id/ingredients", controllers.AddIngredientToRecipe)
		protectedRoutes.DELETE("/recipes/:id/ingredients/:ingredient_id", controllers.DeleteIngredientFromRecipe)

		// Product routes
		protectedRoutes.GET("/products", controllers.GetProducts)
		protectedRoutes.GET("/products/:id", controllers.GetProductByID)
		protectedRoutes.POST("/products", controllers.CreateProduct)
		protectedRoutes.PUT("/products/:id", controllers.UpdateProduct)
		protectedRoutes.DELETE("/products/:id", controllers.DeleteProduct)

		// Price routes
		protectedRoutes.POST("/prices", controllers.AddPrice)
		protectedRoutes.GET("/prices", controllers.GetPrices)

		// Dashboard routes
		protectedRoutes.GET("/dashboard", controllers.GetDashboardData)
		protectedRoutes.GET("/dashboard/profit", controllers.GetProfitData)

		// Client routes
		protectedRoutes.GET("/clients", controllers.GetClients)
		protectedRoutes.GET("/clients/:id", controllers.GetClient)
		protectedRoutes.POST("/clients", controllers.AddClient)
		protectedRoutes.PUT("/clients/:id", controllers.UpdateClient)
		protectedRoutes.DELETE("/clients/:id", controllers.DeleteClient)

		// Order routes
		protectedRoutes.GET("/orders", controllers.GetOrders)
		protectedRoutes.GET("/orders/:id", controllers.GetOrder)
		protectedRoutes.POST("/orders", controllers.AddOrder)
		protectedRoutes.PUT("/orders/:id", controllers.UpdateOrder)
		protectedRoutes.PUT("/orders/:id/status", controllers.UpdateOrderStatus)
		protectedRoutes.DELETE("/orders/:id", controllers.DeleteOrder)

		// Package routes
		protectedRoutes.GET("/packages", controllers.GetPackages)
		protectedRoutes.POST("/packages", controllers.AddPackage)

		// Change password route
		protectedRoutes.POST("/profile/change-password", controllers.ChangePassword)

	}
}
