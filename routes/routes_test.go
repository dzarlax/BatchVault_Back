package routes

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWorkspaceCurrencyUpdateRouteIsDisabledByDefault(t *testing.T) {
	t.Setenv("WORKSPACE_CURRENCY_UPDATES_ENABLED", "")
	router := gin.New()
	SetupRoutes(router)
	if hasRoute(router, http.MethodPatch, "/api/workspaces/current") {
		t.Fatal("workspace currency update route is enabled by default")
	}
}

func TestWorkspaceCurrencyUpdateRouteCanBeEnabled(t *testing.T) {
	t.Setenv("WORKSPACE_CURRENCY_UPDATES_ENABLED", "true")
	router := gin.New()
	SetupRoutes(router)
	if !hasRoute(router, http.MethodPatch, "/api/workspaces/current") {
		t.Fatal("workspace currency update route was not enabled")
	}
}

func hasRoute(router *gin.Engine, method, path string) bool {
	for _, route := range router.Routes() {
		if route.Method == method && route.Path == path {
			return true
		}
	}
	return false
}
