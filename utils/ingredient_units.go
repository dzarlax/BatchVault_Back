package utils

import "strings"

// IngredientUnitScale returns a dimension and an exact integer base-unit multiplier.
func IngredientUnitScale(unit string) (string, int64) {
	normalized := strings.ToLower(strings.TrimSpace(unit))
	switch normalized {
	case "":
		return "count", 1
	case "kg":
		return "mass", 1000
	case "g":
		return "mass", 1
	case "l":
		return "volume", 1000
	case "ml":
		return "volume", 1
	case "pcs":
		return "count", 1
	default:
		return "custom:" + normalized, 1
	}
}
