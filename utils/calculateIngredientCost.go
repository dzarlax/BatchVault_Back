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

	priceDimension, priceFactor := IngredientUnitScale(priceUnit)
	recipeDimension, recipeFactor := IngredientUnitScale(recipeUnit)
	if priceDimension != recipeDimension {
		return 0, fmt.Errorf("incompatible units: %s and %s", priceUnit, recipeUnit)
	}

	basePriceQuantity := priceQuantity * float64(priceFactor)
	baseRecipeQuantity := recipeQuantity * float64(recipeFactor)
	unitPrice := price / basePriceQuantity

	return unitPrice * baseRecipeQuantity, nil
}
