package constants

import (
	"reflect"
	"testing"
)

func TestResolveIngredientUnitContract(t *testing.T) {
	tests := []struct {
		name              string
		ingredientType    string
		workspaceCategory string
		want              UnitContract
	}{
		{
			name:           "spice defaults to fine mass",
			ingredientType: "spice",
			want:           UnitContract{UnitProfile: UnitProfileFineMass, DefaultUnit: "g", AllowedUnits: []string{"g", "kg"}},
		},
		{
			name:              "workspace category overrides ingredient type",
			ingredientType:    "meat",
			workspaceCategory: "oil",
			want:              UnitContract{UnitProfile: UnitProfileVolume, DefaultUnit: "ml", AllowedUnits: []string{"ml", "l"}},
		},
		{
			name:           "count type",
			ingredientType: "packaging",
			want:           UnitContract{UnitProfile: UnitProfileCount, DefaultUnit: "pcs", AllowedUnits: []string{"pcs"}},
		},
		{
			name:           "time type",
			ingredientType: "labor",
			want:           UnitContract{UnitProfile: UnitProfileTime, DefaultUnit: "hh", AllowedUnits: []string{"hh"}},
		},
		{
			name:           "unknown defaults to mass",
			ingredientType: "meat",
			want:           UnitContract{UnitProfile: UnitProfileMass, DefaultUnit: "kg", AllowedUnits: []string{"kg", "g"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveIngredientUnitContract(tt.ingredientType, tt.workspaceCategory)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveIngredientUnitContract() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
