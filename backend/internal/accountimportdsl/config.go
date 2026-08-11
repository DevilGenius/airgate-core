package accountimportdsl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
)

const (
	Version           = 1
	SettingGroup      = "account_import"
	SettingKey        = "account_import_dsl"
	MaxConfigBytes    = 256 << 10
	PriorityMin       = -99999
	PriorityMax       = 99999
	DefaultConfigJSON = "{\n  \"version\": 1,\n  \"rules\": []\n}"
)

type Config struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

type Rule struct {
	Name    string      `json:"name"`
	Enabled *bool       `json:"enabled,omitempty"`
	When    []Condition `json:"when"`
	Set     Assignment  `json:"set"`
}

type Condition struct {
	Field  string   `json:"field"`
	Op     string   `json:"op"`
	Value  string   `json:"value,omitempty"`
	Values []string `json:"values,omitempty"`
}

type Assignment struct {
	MaxConcurrency *int                `json:"max_concurrency,omitempty"`
	Priority       *PriorityAssignment `json:"priority,omitempty"`
	GroupIDs       []int64             `json:"group_ids,omitempty"`
	// 模型降级阈值始终应用：0 表示关闭，0～1 之间的其它值表示开启。
	ModelDowngradeThreshold float64 `json:"model_downgrade_threshold"`
}

// UnmarshalJSON keeps model_downgrade_threshold required and non-null while
// preserving value semantics in the runtime Assignment type.
func (a *Assignment) UnmarshalJSON(data []byte) error {
	type assignmentWire struct {
		MaxConcurrency          *int                `json:"max_concurrency,omitempty"`
		Priority                *PriorityAssignment `json:"priority,omitempty"`
		GroupIDs                []int64             `json:"group_ids,omitempty"`
		ModelDowngradeThreshold *float64            `json:"model_downgrade_threshold"`
	}
	var wire assignmentWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if wire.ModelDowngradeThreshold == nil {
		return errors.New("set 必须提供数值 model_downgrade_threshold，使用 0 表示关闭")
	}
	*a = Assignment{
		MaxConcurrency:          wire.MaxConcurrency,
		Priority:                wire.Priority,
		GroupIDs:                wire.GroupIDs,
		ModelDowngradeThreshold: *wire.ModelDowngradeThreshold,
	}
	return nil
}

type PriorityAssignment struct {
	Mode      string `json:"mode"`
	Value     *int   `json:"value,omitempty"`
	Initial   *int   `json:"initial,omitempty"`
	Step      *int   `json:"step,omitempty"`
	GroupSize *int   `json:"group_size,omitempty"`
	Min       *int   `json:"min,omitempty"`
	Max       *int   `json:"max,omitempty"`
}

func Parse(raw string) (Config, error) {
	if len(raw) > MaxConfigBytes {
		return Config{}, fmt.Errorf("导入配置不能超过 %d KiB", MaxConfigBytes>>10)
	}
	if strings.TrimSpace(raw) == "" {
		raw = DefaultConfigJSON
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("导入配置 JSON 无效: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("导入配置 JSON 无效: %w", err)
	}
	return errors.New("导入配置只能包含一个 JSON 对象")
}

func (c Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("不支持的导入配置版本: %d", c.Version)
	}
	for index, rule := range c.Rules {
		if err := validateRule(index, rule); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(index int, rule Rule) error {
	label := ruleLabel(index, rule.Name)
	if strings.TrimSpace(rule.Name) == "" {
		return fmt.Errorf("%s 缺少 name", label)
	}
	for conditionIndex, condition := range rule.When {
		if err := validateCondition(condition); err != nil {
			return fmt.Errorf("%s 的 when[%d] 无效: %w", label, conditionIndex, err)
		}
	}
	if rule.Set.MaxConcurrency != nil && *rule.Set.MaxConcurrency < 0 {
		return fmt.Errorf("%s 的 max_concurrency 不能小于 0", label)
	}
	if math.IsNaN(rule.Set.ModelDowngradeThreshold) || math.IsInf(rule.Set.ModelDowngradeThreshold, 0) ||
		rule.Set.ModelDowngradeThreshold < 0 || rule.Set.ModelDowngradeThreshold > 1 {
		return fmt.Errorf("%s 的 model_downgrade_threshold 必须在 0～1 范围内", label)
	}
	if err := validatePriority(label, rule.Set.Priority); err != nil {
		return err
	}
	seenGroups := make(map[int64]struct{}, len(rule.Set.GroupIDs))
	for _, groupID := range rule.Set.GroupIDs {
		if groupID <= 0 {
			return fmt.Errorf("%s 的 group_ids 只能包含正整数", label)
		}
		if _, exists := seenGroups[groupID]; exists {
			return fmt.Errorf("%s 的 group_ids 包含重复值 %d", label, groupID)
		}
		seenGroups[groupID] = struct{}{}
	}
	return nil
}

func validateCondition(condition Condition) error {
	field := strings.ToLower(strings.TrimSpace(condition.Field))
	if !isSupportedField(field) {
		return fmt.Errorf("不支持字段 %q", condition.Field)
	}
	op := strings.ToLower(strings.TrimSpace(condition.Op))
	if op == "" {
		op = "eq"
	}
	switch op {
	case "eq", "contains", "prefix", "suffix":
		if strings.TrimSpace(condition.Value) == "" {
			return fmt.Errorf("op=%s 时必须提供 value", op)
		}
	case "in":
		if len(condition.Values) == 0 {
			return errors.New("op=in 时必须提供 values")
		}
	case "empty", "not_empty":
	default:
		return fmt.Errorf("不支持操作符 %q", condition.Op)
	}
	return nil
}

func isSupportedField(field string) bool {
	switch field {
	case "platform", "type", "account_type", "name", "email":
		return true
	default:
		return strings.HasPrefix(field, "credentials.") && len(field) > len("credentials.") ||
			strings.HasPrefix(field, "extra.") && len(field) > len("extra.")
	}
}

func validatePriority(label string, priority *PriorityAssignment) error {
	if priority == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(priority.Mode)) {
	case "fixed":
		if priority.Value == nil || !validPriority(*priority.Value) {
			return fmt.Errorf("%s 的固定优先级必须在 %d～%d 范围内", label, PriorityMin, PriorityMax)
		}
	case "sequence":
		minimum, maximum, err := priorityBounds(priority)
		if err != nil {
			return fmt.Errorf("%s 的序列范围无效: %w", label, err)
		}
		if priority.Initial == nil || *priority.Initial < minimum || *priority.Initial > maximum {
			return fmt.Errorf("%s 的序列 initial 必须在 %d～%d 范围内", label, minimum, maximum)
		}
		if priority.Step == nil || *priority.Step == 0 {
			return fmt.Errorf("%s 的序列 step 不能为 0", label)
		}
		if priority.GroupSize == nil || *priority.GroupSize <= 0 {
			return fmt.Errorf("%s 的序列 group_size 必须大于 0", label)
		}
	default:
		return fmt.Errorf("%s 的 priority.mode 只能是 fixed 或 sequence", label)
	}
	return nil
}

func priorityBounds(priority *PriorityAssignment) (int, int, error) {
	minimum := PriorityMin
	maximum := PriorityMax
	if priority != nil && priority.Min != nil {
		minimum = *priority.Min
	}
	if priority != nil && priority.Max != nil {
		maximum = *priority.Max
	}
	if !validPriority(minimum) || !validPriority(maximum) {
		return 0, 0, fmt.Errorf("min/max 必须在 %d～%d 范围内", PriorityMin, PriorityMax)
	}
	if minimum > maximum {
		return 0, 0, errors.New("min 不能大于 max")
	}
	return minimum, maximum, nil
}

func validPriority(priority int) bool {
	return priority >= PriorityMin && priority <= PriorityMax
}

func (c Config) UsesPrioritySequence() bool {
	for _, rule := range c.Rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		if rule.Set.Priority != nil &&
			strings.EqualFold(strings.TrimSpace(rule.Set.Priority.Mode), "sequence") {
			return true
		}
	}
	return false
}

func (c Config) Apply(items []appaccount.CreateInput) ([]appaccount.CreateInput, error) {
	return c.ApplyWithOccupiedPriorities(items, nil)
}

type sequencePriorityState struct {
	current     int
	assigned    int
	initialized bool
}

func (c Config) ApplyWithOccupiedPriorities(
	items []appaccount.CreateInput,
	occupiedPriorities []int,
) ([]appaccount.CreateInput, error) {
	result := append([]appaccount.CreateInput(nil), items...)
	occupied := make(map[int]struct{}, len(occupiedPriorities)+len(c.Rules))
	for _, priority := range occupiedPriorities {
		occupied[priority] = struct{}{}
	}
	sequenceStates := make([]sequencePriorityState, len(c.Rules))
	for itemIndex := range result {
		for ruleIndex, rule := range c.Rules {
			if rule.Enabled != nil && !*rule.Enabled {
				continue
			}
			if !ruleMatches(rule, result[itemIndex]) {
				continue
			}
			if err := applyAssignment(&result[itemIndex], rule.Set, occupied, &sequenceStates[ruleIndex]); err != nil {
				return nil, fmt.Errorf("%s 应用于账号[%d] %q 失败: %w", ruleLabel(ruleIndex, rule.Name), itemIndex, result[itemIndex].Name, err)
			}
			break
		}
	}
	return result, nil
}

func ruleMatches(rule Rule, item appaccount.CreateInput) bool {
	for _, condition := range rule.When {
		value := fieldValue(item, condition.Field)
		if !conditionMatches(value, condition) {
			return false
		}
	}
	return true
}

func fieldValue(item appaccount.CreateInput, field string) string {
	field = strings.ToLower(strings.TrimSpace(field))
	switch field {
	case "platform":
		return item.Platform
	case "type", "account_type":
		return item.Type
	case "name":
		return item.Name
	case "email":
		if item.Email != nil {
			return *item.Email
		}
	}
	if key, ok := strings.CutPrefix(field, "credentials."); ok {
		return item.Credentials[key]
	}
	if key, ok := strings.CutPrefix(field, "extra."); ok {
		value, exists := item.Extra[key]
		if !exists || value == nil {
			return ""
		}
		switch typed := value.(type) {
		case string:
			return typed
		case bool:
			return strconv.FormatBool(typed)
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case float32:
			return strconv.FormatFloat(float64(typed), 'f', -1, 32)
		case int:
			return strconv.Itoa(typed)
		case int64:
			return strconv.FormatInt(typed, 10)
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func conditionMatches(value string, condition Condition) bool {
	normalizedValue := strings.ToLower(strings.TrimSpace(value))
	op := strings.ToLower(strings.TrimSpace(condition.Op))
	if op == "" {
		op = "eq"
	}
	switch op {
	case "eq":
		return normalizedValue == strings.ToLower(strings.TrimSpace(condition.Value))
	case "in":
		for _, candidate := range condition.Values {
			if normalizedValue == strings.ToLower(strings.TrimSpace(candidate)) {
				return true
			}
		}
		return false
	case "contains":
		return strings.Contains(normalizedValue, strings.ToLower(strings.TrimSpace(condition.Value)))
	case "prefix":
		return strings.HasPrefix(normalizedValue, strings.ToLower(strings.TrimSpace(condition.Value)))
	case "suffix":
		return strings.HasSuffix(normalizedValue, strings.ToLower(strings.TrimSpace(condition.Value)))
	case "empty":
		return normalizedValue == ""
	case "not_empty":
		return normalizedValue != ""
	default:
		return false
	}
}

func applyAssignment(
	item *appaccount.CreateInput,
	assignment Assignment,
	occupied map[int]struct{},
	sequenceState *sequencePriorityState,
) error {
	if assignment.MaxConcurrency != nil {
		item.MaxConcurrency = *assignment.MaxConcurrency
	}
	if assignment.Priority != nil {
		priority, err := assignedPriority(*assignment.Priority, occupied, sequenceState)
		if err != nil {
			return err
		}
		item.Priority = priority
	}
	if assignment.GroupIDs != nil {
		item.GroupIDs = append([]int64(nil), assignment.GroupIDs...)
	}
	item.ModelDowngradeThreshold = assignment.ModelDowngradeThreshold
	return nil
}

func assignedPriority(
	assignment PriorityAssignment,
	occupied map[int]struct{},
	sequenceState *sequencePriorityState,
) (int, error) {
	switch strings.ToLower(strings.TrimSpace(assignment.Mode)) {
	case "fixed":
		return *assignment.Value, nil
	case "sequence":
		return nextSequencePriority(assignment, occupied, sequenceState)
	default:
		return 0, fmt.Errorf("不支持的优先级模式 %q", assignment.Mode)
	}
}

func nextSequencePriority(
	assignment PriorityAssignment,
	occupied map[int]struct{},
	state *sequencePriorityState,
) (int, error) {
	if state == nil {
		return 0, errors.New("缺少优先级序列状态")
	}
	if state.initialized && state.assigned < *assignment.GroupSize {
		state.assigned++
		return state.current, nil
	}

	minimum, maximum, err := priorityBounds(&assignment)
	if err != nil {
		return 0, err
	}
	candidate := int64(*assignment.Initial)
	if state.initialized {
		candidate = int64(state.current) + int64(*assignment.Step)
	}
	for {
		if candidate < int64(minimum) {
			return commitSequencePriority(minimum, occupied, state), nil
		}
		if candidate > int64(maximum) {
			return commitSequencePriority(maximum, occupied, state), nil
		}
		priority := int(candidate)
		if _, exists := occupied[priority]; !exists {
			return commitSequencePriority(priority, occupied, state), nil
		}
		candidate += int64(*assignment.Step)
	}
}

func commitSequencePriority(
	priority int,
	occupied map[int]struct{},
	state *sequencePriorityState,
) int {
	occupied[priority] = struct{}{}
	state.current = priority
	state.assigned = 1
	state.initialized = true
	return priority
}

func ruleLabel(index int, name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return fmt.Sprintf("规则[%d] %q", index, name)
	}
	return fmt.Sprintf("规则[%d]", index)
}
