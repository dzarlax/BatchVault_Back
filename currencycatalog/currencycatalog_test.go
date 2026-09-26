package currencycatalog

import (
	"reflect"
	"testing"
)

func TestListContainsExpectedActiveCurrencies(t *testing.T) {
	list := List()
	want := map[string]string{
		"RSD": "Serbian Dinar",
		"EUR": "Euro",
		"XCG": "Caribbean Guilder",
		"STN": "Dobra",
		"MRU": "Ouguiya",
	}
	if len(list) != 155 {
		t.Fatalf("List() returned %d currencies, want 155 from the pinned SIX snapshot", len(list))
	}
	for _, currency := range list {
		if name, ok := want[currency.Code]; ok && currency.Name != name {
			t.Errorf("%s name = %q, want %q", currency.Code, currency.Name, name)
		}
		delete(want, currency.Code)
	}
	if len(want) != 0 {
		t.Errorf("List() is missing expected currencies: %v", want)
	}
}

func TestIsAllowedRequiresExactMonetaryCode(t *testing.T) {
	for _, code := range []string{"RSD", "EUR", "STN", "XCG", "XOF", "XAF", "XPF"} {
		if !IsAllowed(code) {
			t.Errorf("IsAllowed(%q) = false, want true", code)
		}
	}
	for _, code := range []string{"rsd", "Eur", "BGN", "ANG", "CLF", "BOV", "USN", "XDR", "XUA", "XSU", "XBA", "XTS", "XXX", "XAU", "XAG", ""} {
		if IsAllowed(code) {
			t.Errorf("IsAllowed(%q) = true, want false", code)
		}
	}
}

func TestListReturnsIndependentCopy(t *testing.T) {
	first := List()
	second := List()
	first[0].Code = "BAD"
	if reflect.DeepEqual(first, second) {
		t.Fatal("test setup failed: mutated and fresh lists unexpectedly match")
	}
	if !IsAllowed(second[0].Code) {
		t.Fatalf("mutating a returned list changed catalog membership for %q", second[0].Code)
	}
}
