package utils

import (
	"math"
	"testing"
)

func TestCalculateIngredientCost(t *testing.T) {
	tests := []struct {
		name              string
		price             float64
		priceQuantity     float64
		priceUnit         string
		recipeQuantityStr string
		recipeUnit        string
		want              float64
		wantErr           bool
	}{
		{
			name:              "kilograms to grams",
			price:             10,
			priceQuantity:     1,
			priceUnit:         "kg",
			recipeQuantityStr: "250",
			recipeUnit:        "g",
			want:              2.5,
		},
		{
			name:              "grams to kilograms",
			price:             10,
			priceQuantity:     500,
			priceUnit:         "g",
			recipeQuantityStr: "1",
			recipeUnit:        "kg",
			want:              20,
		},
		{
			name:              "liters to milliliters",
			price:             12,
			priceQuantity:     2,
			priceUnit:         "l",
			recipeQuantityStr: "500",
			recipeUnit:        "ml",
			want:              3,
		},
		{
			name:              "decimal price quantity",
			price:             10,
			priceQuantity:     2.5,
			priceUnit:         "kg",
			recipeQuantityStr: "500",
			recipeUnit:        "g",
			want:              2,
		},
		{
			name:              "milliliters to liters",
			price:             3,
			priceQuantity:     250,
			priceUnit:         "ml",
			recipeQuantityStr: "1",
			recipeUnit:        "l",
			want:              12,
		},
		{
			name:              "comma decimal recipe quantity",
			price:             10,
			priceQuantity:     1,
			priceUnit:         "kg",
			recipeQuantityStr: "0,5",
			recipeUnit:        "kg",
			want:              5,
		},
		{
			name:              "same count unit",
			price:             6,
			priceQuantity:     3,
			priceUnit:         "pcs",
			recipeQuantityStr: "2",
			recipeUnit:        "pcs",
			want:              4,
		},
		{
			name:              "same custom unit",
			price:             10,
			priceQuantity:     2,
			priceUnit:         "bag",
			recipeQuantityStr: "1",
			recipeUnit:        "bag",
			want:              5,
		},
		{
			name:              "empty units count style",
			price:             10,
			priceQuantity:     2,
			priceUnit:         "",
			recipeQuantityStr: "1",
			recipeUnit:        "",
			want:              5,
		},
		{
			name:              "incompatible units",
			price:             10,
			priceQuantity:     1,
			priceUnit:         "kg",
			recipeQuantityStr: "100",
			recipeUnit:        "ml",
			wantErr:           true,
		},
		{
			name:              "zero price quantity",
			price:             10,
			priceQuantity:     0,
			priceUnit:         "kg",
			recipeQuantityStr: "100",
			recipeUnit:        "g",
			wantErr:           true,
		},
		{
			name:              "invalid recipe quantity",
			price:             10,
			priceQuantity:     1,
			priceUnit:         "kg",
			recipeQuantityStr: "abc",
			recipeUnit:        "g",
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateIngredientCost(tt.price, tt.priceQuantity, tt.priceUnit, tt.recipeQuantityStr, tt.recipeUnit)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("CalculateIngredientCost() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("CalculateIngredientCost() error = %v", err)
			}
			if math.Abs(got-tt.want) > 0.000001 {
				t.Fatalf("CalculateIngredientCost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateIngredientCostSupportedConversions(t *testing.T) {
	tests := []struct {
		name          string
		priceQuantity float64
		priceUnit     string
		recipeQty     string
		recipeUnit    string
		want          float64
	}{
		{name: "kg to kg", priceQuantity: 2, priceUnit: "kg", recipeQty: "0.5", recipeUnit: "kg", want: 25},
		{name: "kg to g", priceQuantity: 2, priceUnit: "kg", recipeQty: "500", recipeUnit: "g", want: 25},
		{name: "g to kg", priceQuantity: 500, priceUnit: "g", recipeQty: "1", recipeUnit: "kg", want: 200},
		{name: "g to g", priceQuantity: 500, priceUnit: "g", recipeQty: "250", recipeUnit: "g", want: 50},
		{name: "l to l", priceQuantity: 2, priceUnit: "l", recipeQty: "0.5", recipeUnit: "l", want: 25},
		{name: "l to ml", priceQuantity: 2, priceUnit: "l", recipeQty: "500", recipeUnit: "ml", want: 25},
		{name: "ml to l", priceQuantity: 500, priceUnit: "ml", recipeQty: "1", recipeUnit: "l", want: 200},
		{name: "ml to ml", priceQuantity: 500, priceUnit: "ml", recipeQty: "250", recipeUnit: "ml", want: 50},
		{name: "pcs to pcs", priceQuantity: 4, priceUnit: "pcs", recipeQty: "3", recipeUnit: "pcs", want: 75},
		{name: "case and whitespace normalized", priceQuantity: 2, priceUnit: " KG ", recipeQty: "500", recipeUnit: " G ", want: 25},
		{name: "empty unit to empty unit", priceQuantity: 4, priceUnit: "", recipeQty: "3", recipeUnit: "", want: 75},
		{name: "custom unit to same custom unit", priceQuantity: 4, priceUnit: "bag", recipeQty: "3", recipeUnit: "bag", want: 75},
		{name: "custom unit case and whitespace normalized", priceQuantity: 4, priceUnit: " Bag ", recipeQty: "3", recipeUnit: " bag ", want: 75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateIngredientCost(100, tt.priceQuantity, tt.priceUnit, tt.recipeQty, tt.recipeUnit)
			if err != nil {
				t.Fatalf("CalculateIngredientCost() error = %v", err)
			}
			if math.Abs(got-tt.want) > 0.000001 {
				t.Fatalf("CalculateIngredientCost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateIngredientCostRejectsIncompatibleDimensions(t *testing.T) {
	tests := []struct {
		name       string
		priceUnit  string
		recipeUnit string
	}{
		{name: "mass to volume", priceUnit: "kg", recipeUnit: "ml"},
		{name: "volume to mass", priceUnit: "l", recipeUnit: "g"},
		{name: "count to mass", priceUnit: "pcs", recipeUnit: "g"},
		{name: "mass to count", priceUnit: "g", recipeUnit: "pcs"},
		{name: "count to volume", priceUnit: "pcs", recipeUnit: "ml"},
		{name: "volume to count", priceUnit: "ml", recipeUnit: "pcs"},
		{name: "custom to different custom", priceUnit: "bag", recipeUnit: "box"},
		{name: "empty count to custom", priceUnit: "", recipeUnit: "bag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateIngredientCost(10, 1, tt.priceUnit, "1", tt.recipeUnit)
			if err == nil {
				t.Fatalf("CalculateIngredientCost() error = nil, want error")
			}
		})
	}
}
