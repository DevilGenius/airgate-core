package proxy

import (
	"fmt"
	"strings"
)

const (
	ModeSingle       = "single"
	ModeGroup        = "group"
	AssignmentRandom = "random"
	AssignmentCustom = "custom"
	MaxSlot          = 0xffff
)

// NormalizeMode returns the legacy single-proxy mode when the field is omitted.
func NormalizeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return ModeSingle
	}
	return mode
}

// ValidateConfig validates the common proxy fields and the optional slot range.
func ValidateConfig(mode string, slotStart, slotEnd int) error {
	mode = NormalizeMode(mode)
	if mode != ModeSingle && mode != ModeGroup {
		return ErrInvalidProxyMode
	}
	if mode == ModeSingle {
		return nil
	}
	if slotStart < 0 || slotEnd < slotStart || slotEnd > MaxSlot {
		return ErrInvalidProxySlotRange
	}
	return nil
}

// UsernameForSlot converts a group slot to the provider's four-digit lowercase
// hexadecimal username. Single proxies retain their configured username.
func (p Proxy) UsernameForSlot(slot *int) (string, error) {
	if NormalizeMode(p.Mode) != ModeGroup {
		return p.Username, nil
	}
	if slot == nil || *slot < p.SlotStart || *slot > p.SlotEnd {
		return "", ErrInvalidProxySlotRange
	}
	return FormatSlot(*slot), nil
}

// FormatSlot formats the canonical proxy-group username.
func FormatSlot(slot int) string {
	return fmt.Sprintf("%04x", slot)
}
