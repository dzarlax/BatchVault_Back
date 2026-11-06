package services

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"mobile-backend-go/models"
)

var decimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]{1,3})?$`)

// ParseCostDecimal parses bounded base-ten input without a binary monetary operation.
func ParseCostDecimal(text string) (*big.Rat, error) {
	text = strings.Replace(strings.TrimSpace(text), ",", ".", 1)
	if len(text) > 384 || !decimalPattern.MatchString(text) {
		return nil, fmt.Errorf("invalid decimal number")
	}
	n, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("invalid decimal number")
	}
	return n, nil
}

func costFloat(value float64) (*big.Rat, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("non-finite amount")
	}
	return ParseCostDecimal(strconv.FormatFloat(value, 'f', -1, 64))
}

// ValidateCostDecimal enforces numeric(14,scale), distinguishing a missing value from zero.
func ValidateCostDecimal(value *models.Decimal, scale int, allowZero bool) error {
	if value == nil {
		return nil
	}
	n, err := ParseCostDecimal(string(*value))
	if err != nil || n.Sign() < 0 || (!allowZero && n.Sign() == 0) {
		return fmt.Errorf("invalid decimal value")
	}
	scaled := new(big.Rat).Mul(n, new(big.Rat).SetInt64(int64(math.Pow10(scale))))
	if !scaled.IsInt() || scaled.Num().Cmp(big.NewInt(99999999999999)) > 0 {
		return fmt.Errorf("value exceeds numeric(14,%d) precision", scale)
	}
	return nil
}

func decimalValue(value *models.Decimal) (*big.Rat, error) {
	if value == nil {
		return nil, fmt.Errorf("missing decimal value")
	}
	return ParseCostDecimal(string(*value))
}

// CostCents rounds once, half away from zero, with integer arithmetic.
func CostCents(value *big.Rat) (int64, error) {
	n := new(big.Int).Mul(value.Num(), big.NewInt(100))
	sign := n.Sign()
	n.Abs(n)
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, value.Denom(), r)
	if new(big.Int).Lsh(r, 1).Cmp(value.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if sign < 0 {
		q.Neg(q)
	}
	if !q.IsInt64() || new(big.Int).Abs(new(big.Int).Set(q)).Cmp(big.NewInt(99999999999999)) > 0 {
		return 0, fmt.Errorf("cost exceeds supported range")
	}
	return q.Int64(), nil
}

func roundedCost(value *big.Rat) (*float64, error) {
	cents, err := CostCents(value)
	if err != nil {
		return nil, err
	}
	f := float64(cents) / 100
	return &f, nil
}
func approximateCost(value *big.Rat) *float64 {
	f, _ := value.Float64()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return nil
	}
	return &f
}

// CostAssertionMatches compares a displayed two-decimal assertion to a saved/server amount.
func CostAssertionMatches(assertion, amount float64) bool {
	a, err := costFloat(assertion)
	if err != nil {
		return false
	}
	b, err := costFloat(amount)
	if err != nil {
		return false
	}
	scaled := new(big.Rat).Mul(a, big.NewRat(100, 1))
	if !scaled.IsInt() {
		return false
	}
	bc, err := CostCents(b)
	return err == nil && scaled.Num().IsInt64() && scaled.Num().Int64() == bc
}
