package proxy

import (
	"errors"
	"testing"
)

func TestProxyGroupSlots(t *testing.T) {
	if got := FormatSlot(0x2a); got != "002a" {
		t.Fatalf("FormatSlot() = %q, want 002a", got)
	}
	if err := ValidateConfig(ModeGroup, 0, MaxSlot); err != nil {
		t.Fatalf("ValidateConfig full range: %v", err)
	}
	if err := ValidateConfig(ModeGroup, 2, 1); !errors.Is(err, ErrInvalidProxySlotRange) {
		t.Fatalf("ValidateConfig reversed range = %v", err)
	}
	proxy := Proxy{Mode: ModeGroup, SlotStart: 0, SlotEnd: MaxSlot, Username: "ignored"}
	slot := 0xbeef
	username, err := proxy.UsernameForSlot(&slot)
	if err != nil || username != "beef" {
		t.Fatalf("UsernameForSlot() = %q, %v", username, err)
	}
	if _, err := proxy.UsernameForSlot(nil); !errors.Is(err, ErrInvalidProxySlotRange) {
		t.Fatalf("UsernameForSlot(nil) = %v", err)
	}
}
