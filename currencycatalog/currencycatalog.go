// Package currencycatalog provides the application's offline ISO 4217 currency catalog.
package currencycatalog

import (
	"embed"
	"encoding/json"
	"fmt"
)

// Currency is a monetary ISO 4217 currency code and its SIX-assigned name.
type Currency struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type snapshotFile struct {
	Source     string     `json:"source"`
	Published  string     `json:"published"`
	Currencies []Currency `json:"currencies"`
}

//go:embed snapshots/2026-09-17.json
var snapshotFS embed.FS

var currencies = loadSnapshot()

func loadSnapshot() []Currency {
	data, err := snapshotFS.ReadFile("snapshots/2026-09-17.json")
	if err != nil {
		panic(fmt.Errorf("read embedded currency catalog: %w", err))
	}
	var snapshot snapshotFile
	if err := json.Unmarshal(data, &snapshot); err != nil {
		panic(fmt.Errorf("decode embedded currency catalog: %w", err))
	}
	if len(snapshot.Currencies) == 0 {
		panic("embedded currency catalog is empty")
	}
	return snapshot.Currencies
}

// List returns all supported currencies sorted by ISO code.
func List() []Currency {
	return append([]Currency(nil), currencies...)
}

// IsAllowed reports whether code is an exact, uppercase code in the catalog.
func IsAllowed(code string) bool {
	for _, currency := range currencies {
		if currency.Code == code {
			return true
		}
	}
	return false
}
