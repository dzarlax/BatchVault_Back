package utils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CalculateIngredientCost recalculates the ingredient price taking into account units of measurement
func CalculateIngredientCost(price float64, priceQuantity float64, priceUnit string, recipeQuantityStr string, recipeUnit string) (float64, error) {
	recipeQuantity, err := strconv.ParseFloat(strings.Replace(recipeQuantityStr, ",", ".", 1), 64)
	if err != nil {
		return 0, errors.New("invalid recipe quantity")
	}
	if priceQuantity <= 0 {
		return 0, errors.New("price quantity must be greater than zero")
	}
	if recipeQuantity < 0 {
		return 0, errors.New("recipe quantity cannot be negative")
	}

	priceDimension, priceFactor := normalizeIngredientUnit(priceUnit)
	recipeDimension, recipeFactor := normalizeIngredientUnit(recipeUnit)
	if priceDimension != recipeDimension {
		return 0, fmt.Errorf("incompatible units: %s and %s", priceUnit, recipeUnit)
	}

	basePriceQuantity := priceQuantity * priceFactor
	baseRecipeQuantity := recipeQuantity * recipeFactor
	unitPrice := price / basePriceQuantity

	return unitPrice * baseRecipeQuantity, nil
}

func normalizeIngredientUnit(unit string) (string, float64) {
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
