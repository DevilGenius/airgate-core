package runtimefeatures

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
)

const (
	SettingsGroup          = "system"
	RequestTraceEnabledKey = "request_trace_enabled"
	TextHashEnabledKey     = "text_hash_enabled"
	ImageHashEnabledKey    = "image_hash_enabled"
)

type State struct {
	RequestTraceEnabled bool
	TextHashEnabled     bool
	ImageHashEnabled    bool
}

type Patch struct {
	RequestTraceEnabled *bool
	TextHashEnabled     *bool
	ImageHashEnabled    *bool
}

type Applier interface {
	ApplyRuntimeFeatures(context.Context, State) error
}

type Controller struct {
	mu       sync.Mutex
	settings *appsettings.Service
	applier  Applier
	state    State
}

func DefaultState() State {
	return State{
		RequestTraceEnabled: false,
		TextHashEnabled:     true,
		ImageHashEnabled:    true,
	}
}

func LoadOrInitialize(ctx context.Context, settings *appsettings.Service) (State, error) {
	if settings == nil {
		return State{}, fmt.Errorf("settings service is unavailable")
	}
	items, err := settings.List(ctx, SettingsGroup)
	if err != nil {
		return State{}, fmt.Errorf("load runtime feature settings: %w", err)
	}

	state := DefaultState()
	found := make(map[string]bool, 3)
	updates := make([]appsettings.ItemInput, 0, 3)
	for _, item := range items {
		defaultValue, known := runtimeFeatureDefault(item.Key)
		if !known {
			continue
		}
		found[item.Key] = true
		raw := strings.TrimSpace(item.Value)
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			value = defaultValue
		}
		setStateValue(&state, item.Key, value)
		normalized := strconv.FormatBool(value)
		if parseErr != nil || raw != normalized || item.Group != SettingsGroup {
			updates = append(updates, appsettings.ItemInput{
				Key:   item.Key,
				Value: normalized,
				Group: SettingsGroup,
			})
		}
	}
	for _, key := range []string{RequestTraceEnabledKey, TextHashEnabledKey, ImageHashEnabledKey} {
		if found[key] {
			continue
		}
		value, _ := runtimeFeatureDefault(key)
		setStateValue(&state, key, value)
		updates = append(updates, appsettings.ItemInput{
			Key:   key,
			Value: strconv.FormatBool(value),
			Group: SettingsGroup,
		})
	}
	if len(updates) > 0 {
		if err := settings.Update(ctx, updates); err != nil {
			return State{}, fmt.Errorf("initialize runtime feature settings: %w", err)
		}
	}
	return state, nil
}

func NewController(settings *appsettings.Service, applier Applier, initial State) *Controller {
	return &Controller{
		settings: settings,
		applier:  applier,
		state:    initial,
	}
}

func (c *Controller) State() State {
	if c == nil {
		return State{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Controller) Update(ctx context.Context, patch Patch) (State, error) {
	if c == nil || c.settings == nil || c.applier == nil {
		return State{}, fmt.Errorf("runtime feature controller is unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	current := c.state
	next := applyPatch(current, patch)
	if next == current {
		return current, nil
	}
	if err := c.applier.ApplyRuntimeFeatures(ctx, next); err != nil {
		return current, fmt.Errorf("apply runtime feature state: %w", err)
	}
	if err := c.settings.Update(ctx, stateItems(next)); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		rollbackErr := c.applier.ApplyRuntimeFeatures(rollbackCtx, current)
		cancel()
		if rollbackErr != nil {
			return current, fmt.Errorf("persist runtime feature state: %w; rollback runtime state: %v", err, rollbackErr)
		}
		return current, fmt.Errorf("persist runtime feature state: %w", err)
	}
	c.state = next
	return next, nil
}

func applyPatch(state State, patch Patch) State {
	if patch.RequestTraceEnabled != nil {
		state.RequestTraceEnabled = *patch.RequestTraceEnabled
	}
	if patch.TextHashEnabled != nil {
		state.TextHashEnabled = *patch.TextHashEnabled
	}
	if patch.ImageHashEnabled != nil {
		state.ImageHashEnabled = *patch.ImageHashEnabled
	}
	return state
}

func stateItems(state State) []appsettings.ItemInput {
	return []appsettings.ItemInput{
		{
			Key:   RequestTraceEnabledKey,
			Value: strconv.FormatBool(state.RequestTraceEnabled),
			Group: SettingsGroup,
		},
		{
			Key:   TextHashEnabledKey,
			Value: strconv.FormatBool(state.TextHashEnabled),
			Group: SettingsGroup,
		},
		{
			Key:   ImageHashEnabledKey,
			Value: strconv.FormatBool(state.ImageHashEnabled),
			Group: SettingsGroup,
		},
	}
}

func runtimeFeatureDefault(key string) (bool, bool) {
	switch key {
	case RequestTraceEnabledKey:
		return false, true
	case TextHashEnabledKey, ImageHashEnabledKey:
		return true, true
	default:
		return false, false
	}
}

func setStateValue(state *State, key string, value bool) {
	if state == nil {
		return
	}
	switch key {
	case RequestTraceEnabledKey:
		state.RequestTraceEnabled = value
	case TextHashEnabledKey:
		state.TextHashEnabled = value
	case ImageHashEnabledKey:
		state.ImageHashEnabled = value
	}
}
