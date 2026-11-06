package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Decimal preserves a PostgreSQL numeric value without a binary-float round trip.
type Decimal string

func (d Decimal) Value() (driver.Value, error) { return string(d), nil }
func (d *Decimal) Scan(value interface{}) error {
	switch v := value.(type) {
	case string:
		*d = Decimal(v)
	case []byte:
		*d = Decimal(string(v))
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("non-finite decimal")
		}
		*d = Decimal(strconv.FormatFloat(v, 'f', -1, 64))
	case int64:
		*d = Decimal(strconv.FormatInt(v, 10))
	default:
		return fmt.Errorf("unsupported decimal database value %T", value)
	}
	return nil
}
func (d Decimal) MarshalJSON() ([]byte, error) { return json.Marshal(json.Number(d)) }
func (d *Decimal) UnmarshalJSON(data []byte) error {
	var number json.Number
	if len(data) > 128 || string(data) == "null" || len(data) == 0 || data[0] == '"' {
		return fmt.Errorf("expected a decimal number")
	}
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*d = Decimal(number.String())
	return nil
}

// OptionalDecimal distinguishes omitted fields, explicit NULL and a known zero.
type OptionalDecimal struct {
	Set   bool     `json:"-"`
	Value *Decimal `json:"-"`
}

func (o *OptionalDecimal) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var d Decimal
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	o.Value = &d
	return nil
}
