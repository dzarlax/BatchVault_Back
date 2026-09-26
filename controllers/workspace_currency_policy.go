package controllers

import (
	"os"
	"strings"
)

// WorkspaceCurrencyUpdatesEnabled reports whether workspace currency updates are enabled.
// The endpoint remains disabled by default until the workspace currency rollout is enabled.
func WorkspaceCurrencyUpdatesEnabled() bool {
	return strings.EqualFold(os.Getenv("WORKSPACE_CURRENCY_UPDATES_ENABLED"), "true")
}
