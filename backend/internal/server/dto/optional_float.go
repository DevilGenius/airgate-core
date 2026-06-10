package dto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// OptionalFloat preserves JSON field presence for patch-style request DTOs.
//
// Missing field: Set=false.
// Explicit null or empty string: Set=true, Null=true.
// Number: Set=true, Null=false, Value=<number>.
type OptionalFloat struct {
	Set   bool
	Null  bool
	Value float64
}

func NewOptionalFloat(value float64) OptionalFloat {
	return OptionalFloat{Set: true, Value: value}
}

func (o *OptionalFloat) UnmarshalJSON(data []byte) error {
	o.Set = true
	o.Null = false
	o.Value = 0

	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		o.Null = true
		return nil
	}

	var rawString string
	if len(trimmed) >= 2 && trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &rawString); err != nil {
			return err
		}
		if strings.TrimSpace(rawString) == "" {
			o.Null = true
			return nil
		}
		return fmt.Errorf("must be a number or null")
	}

	var value float64
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return err
	}
	o.Value = value
	return nil
}

func (o OptionalFloat) MarshalJSON() ([]byte, error) {
	if !o.Set || o.Null {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

func (o OptionalFloat) Ptr() *float64 {
	if !o.Set || o.Null {
		return nil
	}
	value := o.Value
	return &value
}

func (o OptionalFloat) PtrOrDefault(defaultValue float64) *float64 {
	if !o.Set {
		return nil
	}
	if o.Null {
		value := defaultValue
		return &value
	}
	value := o.Value
	return &value
}
