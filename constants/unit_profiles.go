package constants

import "strings"

const (
	UnitProfileMass     = "mass"
	UnitProfileFineMass = "fine_mass"
	UnitProfileVolume   = "volume"
	UnitProfileCount    = "count"
	UnitProfileTime     = "time"
)

// UnitContract describes the canonical unit choices clients should offer.
type UnitContract struct {
	UnitProfile  string
	DefaultUnit  string
	AllowedUnits []string
}

// ResolveIngredientUnitContract maps existing ingredient metadata to a stable,
// additive unit contract. It intentionally stays permissive and schema-free.
func ResolveIngredientUnitContract(ingredientType string, workspaceCategory string) UnitContract {
	normalized := normalizeUnitProfileHint(workspaceCategory)
	if normalized == "" {
		normalized = normalizeUnitProfileHint(ingredientType)
	}

	switch normalized {
	case "spice", "seasoning", "powder", "salt", "sugar":
		return UnitContract{UnitProfile: UnitProfileFineMass, DefaultUnit: "g", AllowedUnits: []string{"g", "kg"}}
	case "sauce", "liquid", "oil", "water", "marinade", "brine":
		return UnitContract{UnitProfile: UnitProfileVolume, DefaultUnit: "ml", AllowedUnits: []string{"ml", "l"}}
	case "piece", "pieces", "count", "unit", "units", "package", "packaging", "tray", "bag":
		return UnitContract{UnitProfile: UnitProfileCount, DefaultUnit: "pcs", AllowedUnits: []string{"pcs"}}
	case "time", "labor", "labour", "work", "process":
		return UnitContract{UnitProfile: UnitProfileTime, DefaultUnit: "hh", AllowedUnits: []string{"hh"}}
	default:
		return UnitContract{UnitProfile: UnitProfileMass, DefaultUnit: "kg", AllowedUnits: []string{"kg", "g"}}
	}
}

func normalizeUnitProfileHint(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
