package dto

import (
	"encoding/json"
	"testing"
)

func TestOptionalFloatUnmarshalDistinguishesMissingNullAndZero(t *testing.T) {
	var payload struct {
		RateMultiplier OptionalFloat `json:"rate_multiplier"`
	}
	if err := json.Unmarshal([]byte(`{}`), &payload); err != nil {
		t.Fatalf("unmarshal missing field: %v", err)
	}
	if payload.RateMultiplier.Set {
		t.Fatalf("missing field should leave Set=false")
	}

	if err := json.Unmarshal([]byte(`{"rate_multiplier":null}`), &payload); err != nil {
		t.Fatalf("unmarshal null field: %v", err)
	}
	if !payload.RateMultiplier.Set || !payload.RateMultiplier.Null {
		t.Fatalf("null field = %+v, want Set=true Null=true", payload.RateMultiplier)
	}

	if err := json.Unmarshal([]byte(`{"rate_multiplier":""}`), &payload); err != nil {
		t.Fatalf("unmarshal empty string field: %v", err)
	}
	if !payload.RateMultiplier.Set || !payload.RateMultiplier.Null {
		t.Fatalf("empty string field = %+v, want Set=true Null=true", payload.RateMultiplier)
	}

	if err := json.Unmarshal([]byte(`{"rate_multiplier":0}`), &payload); err != nil {
		t.Fatalf("unmarshal zero field: %v", err)
	}
	if !payload.RateMultiplier.Set || payload.RateMultiplier.Null || payload.RateMultiplier.Value != 0 {
		t.Fatalf("zero field = %+v, want Set=true Null=false Value=0", payload.RateMultiplier)
	}

	if err := json.Unmarshal([]byte(`{"rate_multiplier":0.001}`), &payload); err != nil {
		t.Fatalf("unmarshal min positive field: %v", err)
	}
	if !payload.RateMultiplier.Set || payload.RateMultiplier.Null || payload.RateMultiplier.Value != 0.001 {
		t.Fatalf("min positive field = %+v, want Set=true Null=false Value=0.001", payload.RateMultiplier)
	}
}

func TestOptionalFloatUpdatePointerMapping(t *testing.T) {
	if got := (OptionalFloat{}).PtrOrDefault(1); got != nil {
		t.Fatalf("missing field PtrOrDefault = %v, want nil", *got)
	}

	nullValue := OptionalFloat{Set: true, Null: true}
	if got := nullValue.PtrOrDefault(1); got == nil || *got != 1 {
		t.Fatalf("null field PtrOrDefault = %v, want 1", got)
	}

	zeroValue := OptionalFloat{Set: true, Value: 0}
	if got := zeroValue.PtrOrDefault(1); got == nil || *got != 0 {
		t.Fatalf("zero field PtrOrDefault = %v, want 0", got)
	}
}

func TestOptionalFloatMarshalAsNumber(t *testing.T) {
	payload, err := json.Marshal(struct {
		RateMultiplier OptionalFloat `json:"rate_multiplier"`
	}{RateMultiplier: NewOptionalFloat(0)})
	if err != nil {
		t.Fatalf("marshal optional float: %v", err)
	}
	if string(payload) != `{"rate_multiplier":0}` {
		t.Fatalf("payload = %s, want numeric rate_multiplier", payload)
	}
}
