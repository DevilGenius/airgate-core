package routegraph

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	entapikey "github.com/DevilGenius/airgate-core/ent/apikey"
	entgroup "github.com/DevilGenius/airgate-core/ent/group"
	entuser "github.com/DevilGenius/airgate-core/ent/user"
	"github.com/DevilGenius/airgate-core/internal/accountpriority"
	"github.com/DevilGenius/airgate-core/internal/accountscope"
	"github.com/DevilGenius/airgate-core/internal/dispatchresolver"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type Snapshot struct {
	groupsByID       map[int]*GroupNode
	groupsByPlatform map[string][]*GroupNode
	usersByID        map[int]*UserNode
	apiKeysByID      map[int]*APIKeyNode
	refreshedAt      time.Time
}

type GroupNode struct {
	ID                       int
	Name                     string
	Platform                 string
	RateMultiplier           float64
	IsExclusive              bool
	AllowedUsers             map[int]struct{}
	ModelRouting             map[string][]int64
	ModelPolicy              modelpolicy.Policy
	AccountTypeModelPolicies map[string]modelpolicy.Policy
	DispatchDSL              sdk.DispatchDSL
	DispatchResolver         *dispatchresolver.CompiledResolver
	OperationPolicies        map[string]bool
	PluginSettings           map[string]map[string]string
	ServiceTier              string
	ForceInstructions        string
	SortWeight               int
	UpdatedAt                time.Time
	Accounts                 []*AccountNode

	accountRefs         []*ent.Account
	modelPolicy         modelpolicy.Compiled
	accountTypePolicies []compiledAccountTypePolicy
	hasModelConstraints bool
}

type AccountNode struct {
	Account     *ent.Account
	ModelPolicy modelpolicy.Policy

	modelPolicy  modelpolicy.Compiled
	categoryKeys []string
}

const (
	accountGroupPrioritiesExtraKey = "group_priorities"
)

type compiledAccountTypePolicy struct {
	key    string
	policy modelpolicy.Compiled
}

var knownAccountCategoryAliases = map[string]struct{}{
	"free":       {},
	"plus":       {},
	"pro":        {},
	"team":       {},
	"enterprise": {},
}

var accountCategoryTokenAliases = map[string][]string{
	"k12": {"team"},
}

type UserNode struct {
	ID             int
	Email          string
	Status         entuser.Status
	Balance        float64
	GroupRates     map[int64]float64
	MaxConcurrency int
}

type APIKeyNode struct {
	ID        int
	UserID    int
	Status    entapikey.Status
	QuotaUSD  float64
	UsedQuota float64
	SellRate  float64
	ExpiresAt *time.Time
}

type APIKeyInactiveReason string

const (
	APIKeyInactiveNone      APIKeyInactiveReason = ""
	APIKeyInactiveMissing   APIKeyInactiveReason = "missing"
	APIKeyInactiveDisabled  APIKeyInactiveReason = "disabled"
	APIKeyInactiveExpired   APIKeyInactiveReason = "expired"
	APIKeyInactiveExhausted APIKeyInactiveReason = "quota_exhausted"
)

func (u *UserNode) Active() bool {
	return u != nil && u.Status == entuser.StatusActive
}

func (k *APIKeyNode) Active(now time.Time) bool {
	return k.InactiveReason(now) == APIKeyInactiveNone
}

func (k *APIKeyNode) InactiveReason(now time.Time) APIKeyInactiveReason {
	if k == nil {
		return APIKeyInactiveMissing
	}
	if k.Status != entapikey.StatusActive {
		return APIKeyInactiveDisabled
	}
	if k.ExpiresAt != nil && !k.ExpiresAt.After(now) {
		return APIKeyInactiveExpired
	}
	if k.QuotaExhausted() {
		return APIKeyInactiveExhausted
	}
	return APIKeyInactiveNone
}

func (k *APIKeyNode) QuotaExhausted() bool {
	return k != nil && k.QuotaUSD > 0 && k.UsedQuota >= k.QuotaUSD
}

var (
	snapshotValue atomic.Value // *Snapshot
	updateMu      sync.Mutex

	queryRefreshUsers = func(ctx context.Context, db *ent.Client) ([]*ent.User, error) {
		return db.User.Query().All(ctx)
	}
	queryRefreshAPIKeys = func(ctx context.Context, db *ent.Client) ([]*ent.APIKey, error) {
		return db.APIKey.Query().WithUser().All(ctx)
	}
)

func Current() *Snapshot {
	value := snapshotValue.Load()
	if value == nil {
		return nil
	}
	return value.(*Snapshot)
}

func RefreshSync(ctx context.Context, db *ent.Client) error {
	if db == nil {
		return nil
	}
	groups, err := db.Group.Query().
		WithAllowedUsers(func(q *ent.UserQuery) {
			q.Select(entuser.FieldID)
		}).
		WithAccounts(func(q *ent.AccountQuery) {
			q.Where(accountscope.NotDeleted())
			q.WithProxy()
		}).
		All(ctx)
	if err != nil {
		return err
	}

	users, err := queryRefreshUsers(ctx, db)
	if err != nil {
		return err
	}

	apiKeys, err := queryRefreshAPIKeys(ctx, db)
	if err != nil {
		return err
	}

	next := newEmptySnapshot()
	next.groupsByID = make(map[int]*GroupNode, len(groups))
	next.usersByID = make(map[int]*UserNode, len(users))
	next.apiKeysByID = make(map[int]*APIKeyNode, len(apiKeys))

	for _, user := range users {
		next.usersByID[user.ID] = buildUserNode(user)
	}
	for _, key := range apiKeys {
		next.apiKeysByID[key.ID] = buildAPIKeyNode(key)
	}
	for _, group := range groups {
		putGroupNode(next, buildGroupNode(group))
	}

	updateMu.Lock()
	snapshotValue.Store(next)
	updateMu.Unlock()
	return nil
}

func RefreshGroup(ctx context.Context, db *ent.Client, groupID int) error {
	if db == nil || groupID <= 0 {
		return nil
	}
	group, err := db.Group.Query().
		Where(entgroup.IDEQ(groupID)).
		WithAllowedUsers(func(q *ent.UserQuery) {
			q.Select(entuser.FieldID)
		}).
		WithAccounts(func(q *ent.AccountQuery) {
			q.Where(accountscope.NotDeleted())
			q.WithProxy()
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			RemoveGroup(groupID)
			return nil
		}
		return err
	}
	node := buildGroupNode(group)
	updateSnapshot(func(next *Snapshot) {
		putGroupNode(next, node)
	})
	return nil
}

func RemoveGroup(groupID int) {
	if groupID <= 0 {
		return
	}
	updateSnapshot(func(next *Snapshot) {
		removeGroupNode(next, groupID)
	})
}

func RefreshAccount(ctx context.Context, db *ent.Client, accountID int) error {
	if db == nil || accountID <= 0 {
		return nil
	}
	account, err := accountscope.QueryByID(db, accountID).
		WithGroups().
		WithProxy().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			RemoveAccount(accountID)
			return nil
		}
		return err
	}
	node := buildAccountNode(account)
	updateSnapshot(func(next *Snapshot) {
		putAccountNode(next, node)
	})
	return nil
}

func RemoveAccount(accountID int) {
	if accountID <= 0 {
		return
	}
	updateSnapshot(func(next *Snapshot) {
		removeAccountNode(next, accountID)
	})
}

func RefreshUser(ctx context.Context, db *ent.Client, userID int) error {
	if db == nil || userID <= 0 {
		return nil
	}
	user, err := db.User.Query().
		Where(entuser.IDEQ(userID)).
		WithAllowedGroups(func(q *ent.GroupQuery) {
			q.Select(entgroup.FieldID)
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			RemoveUser(userID)
			return nil
		}
		return err
	}
	allowedGroupIDs := make(map[int]struct{}, len(user.Edges.AllowedGroups))
	for _, group := range user.Edges.AllowedGroups {
		allowedGroupIDs[group.ID] = struct{}{}
	}
	node := buildUserNode(user)
	updateSnapshot(func(next *Snapshot) {
		next.usersByID[user.ID] = node
		replaceAllowedUser(next, user.ID, allowedGroupIDs)
	})
	return nil
}

func RemoveUser(userID int) {
	if userID <= 0 {
		return
	}
	updateSnapshot(func(next *Snapshot) {
		delete(next.usersByID, userID)
		replaceAllowedUser(next, userID, nil)
		for keyID, key := range next.apiKeysByID {
			if key.UserID == userID {
				delete(next.apiKeysByID, keyID)
			}
		}
	})
}

func RefreshAPIKey(ctx context.Context, db *ent.Client, keyID int) error {
	if db == nil || keyID <= 0 {
		return nil
	}
	key, err := db.APIKey.Query().
		Where(entapikey.IDEQ(keyID)).
		WithUser().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			RemoveAPIKey(keyID)
			return nil
		}
		return err
	}
	node := buildAPIKeyNode(key)
	updateSnapshot(func(next *Snapshot) {
		next.apiKeysByID[key.ID] = node
	})
	return nil
}

func RemoveAPIKey(keyID int) {
	if keyID <= 0 {
		return
	}
	updateSnapshot(func(next *Snapshot) {
		delete(next.apiKeysByID, keyID)
	})
}

func Group(id int) *GroupNode {
	snapshot := Current()
	if snapshot == nil {
		return nil
	}
	return snapshot.groupsByID[id]
}

func GroupsByPlatform(platform string) []*GroupNode {
	snapshot := Current()
	if snapshot == nil {
		return nil
	}
	return snapshot.groupsByPlatform[platform]
}

func User(id int) *UserNode {
	snapshot := Current()
	if snapshot == nil {
		return nil
	}
	return snapshot.usersByID[id]
}

func APIKey(id int) *APIKeyNode {
	snapshot := Current()
	if snapshot == nil {
		return nil
	}
	return snapshot.apiKeysByID[id]
}

// SetSnapshotForTesting replaces the global snapshot for tests and returns a restore callback.
func SetSnapshotForTesting(groups []*ent.Group) func() {
	updateMu.Lock()
	defer updateMu.Unlock()

	previous := Current()
	next := newEmptySnapshot()
	for _, group := range groups {
		putGroupNode(next, buildGroupNode(group))
	}
	snapshotValue.Store(next)
	return func() {
		updateMu.Lock()
		defer updateMu.Unlock()
		if previous == nil {
			snapshotValue.Store(newEmptySnapshot())
			return
		}
		snapshotValue.Store(previous)
	}
}

func (g *GroupNode) AccountsForModel(model string) []*ent.Account {
	if g == nil {
		return nil
	}
	if !g.hasModelConstraints {
		return g.accountRefs
	}
	if !g.modelPolicy.Allows(model) {
		return nil
	}

	var allowedIDs map[int64]struct{}
	if len(g.ModelRouting) > 0 {
		ids := matchModelRouting(g.ModelRouting, model)
		if len(ids) == 0 {
			return nil
		}
		allowedIDs = make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			allowedIDs[id] = struct{}{}
		}
	}

	out := make([]*ent.Account, 0, len(g.Accounts))
	for _, node := range g.Accounts {
		if node == nil || node.Account == nil {
			continue
		}
		if allowedIDs != nil {
			if _, ok := allowedIDs[int64(node.Account.ID)]; !ok {
				continue
			}
		}
		if !g.accountTypeAllows(node, model) {
			continue
		}
		if !node.modelPolicy.Allows(model) {
			continue
		}
		out = append(out, node.Account)
	}
	return out
}

// HasAccountForModel reports whether at least one account can serve model after static policies.
func (g *GroupNode) HasAccountForModel(model string) bool {
	if g == nil {
		return false
	}
	if !g.hasModelConstraints {
		return len(g.accountRefs) > 0
	}
	if !g.modelPolicy.Allows(model) {
		return false
	}

	var allowedIDs map[int64]struct{}
	if len(g.ModelRouting) > 0 {
		ids := matchModelRouting(g.ModelRouting, model)
		if len(ids) == 0 {
			return false
		}
		allowedIDs = make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			allowedIDs[id] = struct{}{}
		}
	}

	for _, node := range g.Accounts {
		if node == nil || node.Account == nil {
			continue
		}
		if allowedIDs != nil {
			if _, ok := allowedIDs[int64(node.Account.ID)]; !ok {
				continue
			}
		}
		if !g.accountTypeAllows(node, model) {
			continue
		}
		if !node.modelPolicy.Allows(model) {
			continue
		}
		return true
	}
	return false
}

func (g *GroupNode) accountTypeAllows(account *AccountNode, model string) bool {
	if len(g.accountTypePolicies) == 0 {
		return true
	}
	for _, policy := range g.accountTypePolicies {
		if !account.matchesCategory(policy.key) {
			continue
		}
		if !policy.policy.Allows(model) {
			return false
		}
	}
	return true
}

func (a *AccountNode) matchesCategory(key string) bool {
	if a == nil || key == "" {
		return false
	}
	normalizedKey := normalizeCategory(key)
	if normalizedKey == "" {
		return false
	}
	for _, category := range a.categoryKeys {
		if category == normalizedKey {
			return true
		}
	}
	return false
}

func updateSnapshot(mutator func(*Snapshot)) {
	updateMu.Lock()
	defer updateMu.Unlock()

	next := cloneSnapshot(Current())
	mutator(next)
	next.refreshedAt = time.Now()
	snapshotValue.Store(next)
}

func newEmptySnapshot() *Snapshot {
	return &Snapshot{
		groupsByID:       make(map[int]*GroupNode),
		groupsByPlatform: make(map[string][]*GroupNode),
		usersByID:        make(map[int]*UserNode),
		apiKeysByID:      make(map[int]*APIKeyNode),
		refreshedAt:      time.Now(),
	}
}

func cloneSnapshot(base *Snapshot) *Snapshot {
	if base == nil {
		return newEmptySnapshot()
	}
	next := &Snapshot{
		groupsByID:       make(map[int]*GroupNode, len(base.groupsByID)),
		groupsByPlatform: make(map[string][]*GroupNode, len(base.groupsByPlatform)),
		usersByID:        make(map[int]*UserNode, len(base.usersByID)),
		apiKeysByID:      make(map[int]*APIKeyNode, len(base.apiKeysByID)),
		refreshedAt:      base.refreshedAt,
	}
	for id, group := range base.groupsByID {
		next.groupsByID[id] = group
	}
	for platform, groups := range base.groupsByPlatform {
		next.groupsByPlatform[platform] = append([]*GroupNode(nil), groups...)
	}
	for id, user := range base.usersByID {
		next.usersByID[id] = user
	}
	for id, key := range base.apiKeysByID {
		next.apiKeysByID[id] = key
	}
	return next
}

func buildGroupNode(group *ent.Group) *GroupNode {
	node := &GroupNode{
		ID:                       group.ID,
		Name:                     group.Name,
		Platform:                 group.Platform,
		RateMultiplier:           group.RateMultiplier,
		IsExclusive:              group.IsExclusive,
		AllowedUsers:             make(map[int]struct{}, len(group.Edges.AllowedUsers)),
		ModelRouting:             cloneModelRouting(group.ModelRouting),
		ModelPolicy:              modelpolicy.Clone(group.ModelPolicy),
		AccountTypeModelPolicies: modelpolicy.CloneMap(group.AccountTypeModelPolicies),
		DispatchDSL:              cloneDispatchDSL(group.DispatchDsl),
		OperationPolicies:        cloneOperationPolicies(group.OperationPolicies),
		PluginSettings:           clonePluginSettings(group.PluginSettings),
		ServiceTier:              group.ServiceTier,
		ForceInstructions:        group.ForceInstructions,
		SortWeight:               group.SortWeight,
		UpdatedAt:                group.UpdatedAt,
	}
	node.modelPolicy = modelpolicy.Compile(node.ModelPolicy)
	node.accountTypePolicies = compileAccountTypePolicies(node.AccountTypeModelPolicies)
	node.DispatchResolver = dispatchresolver.CompileCached(groupDispatchResolverCacheKey(group.ID, group.UpdatedAt), node.DispatchDSL)
	for _, allowed := range group.Edges.AllowedUsers {
		node.AllowedUsers[allowed.ID] = struct{}{}
	}
	accounts := make([]*AccountNode, 0, len(group.Edges.Accounts))
	for _, account := range group.Edges.Accounts {
		if account.Platform != group.Platform {
			continue
		}
		accounts = append(accounts, buildAccountNode(account))
	}
	return withAccountNodes(node, accounts)
}

func buildAccountNode(account *ent.Account) *AccountNode {
	policy := account.ModelPolicy
	return &AccountNode{
		Account:      account,
		ModelPolicy:  modelpolicy.Clone(policy),
		modelPolicy:  modelpolicy.Compile(policy),
		categoryKeys: accountCategoryKeys(account),
	}
}

func buildUserNode(user *ent.User) *UserNode {
	return &UserNode{
		ID:             user.ID,
		Email:          user.Email,
		Status:         user.Status,
		Balance:        user.Balance,
		GroupRates:     cloneGroupRates(user.GroupRates),
		MaxConcurrency: user.MaxConcurrency,
	}
}

func buildAPIKeyNode(key *ent.APIKey) *APIKeyNode {
	userID := 0
	if key.Edges.User != nil {
		userID = key.Edges.User.ID
	}
	return &APIKeyNode{
		ID:        key.ID,
		UserID:    userID,
		Status:    key.Status,
		QuotaUSD:  key.QuotaUsd,
		UsedQuota: key.UsedQuota,
		SellRate:  key.SellRate,
		ExpiresAt: key.ExpiresAt,
	}
}

func compileAccountTypePolicies(input map[string]modelpolicy.Policy) []compiledAccountTypePolicy {
	if len(input) == 0 {
		return nil
	}
	policies := make([]compiledAccountTypePolicy, 0, len(input))
	for key, policy := range input {
		compiled := modelpolicy.Compile(policy)
		if !compiled.Restricts() {
			continue
		}
		policies = append(policies, compiledAccountTypePolicy{
			key:    key,
			policy: compiled,
		})
	}
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].key < policies[j].key
	})
	return policies
}

func withAccountNodes(group *GroupNode, accounts []*AccountNode) *GroupNode {
	next := *group
	next.Accounts = make([]*AccountNode, 0, len(accounts))
	next.accountRefs = make([]*ent.Account, 0, len(accounts))
	next.hasModelConstraints = len(next.ModelRouting) > 0 ||
		next.modelPolicy.Restricts() ||
		len(next.accountTypePolicies) > 0
	for _, account := range accounts {
		if account == nil || account.Account == nil {
			next.Accounts = append(next.Accounts, account)
			continue
		}
		groupAccount := accountNodeForGroup(next.ID, account)
		next.Accounts = append(next.Accounts, groupAccount)
		next.accountRefs = append(next.accountRefs, groupAccount.Account)
		if groupAccount.modelPolicy.Restricts() {
			next.hasModelConstraints = true
		}
	}
	return &next
}

func accountNodeForGroup(groupID int, account *AccountNode) *AccountNode {
	if account == nil || account.Account == nil {
		return account
	}
	priority, ok := accountGroupPriorityOverride(account.Account.Extra, groupID)
	if !ok || priority == account.Account.Priority {
		return account
	}
	entAccount := *account.Account
	entAccount.Priority = priority
	next := *account
	next.Account = &entAccount
	return &next
}

func accountGroupPriorityOverride(extra map[string]interface{}, groupID int) (int, bool) {
	if len(extra) == 0 || groupID <= 0 {
		return 0, false
	}
	raw, ok := extra[accountGroupPrioritiesExtraKey]
	if !ok {
		return 0, false
	}
	priority, ok := groupPriorityValue(raw, strconv.Itoa(groupID))
	if !ok {
		return 0, false
	}
	return clampAccountPriority(priority), true
}

func groupPriorityValue(raw interface{}, groupID string) (int, bool) {
	switch values := raw.(type) {
	case map[string]interface{}:
		return priorityValue(values[groupID])
	case map[string]int:
		priority, ok := values[groupID]
		return priority, ok
	case map[string]int64:
		priority, ok := values[groupID]
		return int(priority), ok
	case map[string]float64:
		priority, ok := values[groupID]
		return int(math.Round(priority)), ok
	default:
		return 0, false
	}
}

func priorityValue(raw interface{}) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(math.Round(value)), true
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func clampAccountPriority(value int) int {
	return accountpriority.Clamp(value)
}

func putGroupNode(snapshot *Snapshot, group *GroupNode) {
	if snapshot == nil || group == nil {
		return
	}
	removeGroupNode(snapshot, group.ID)
	snapshot.groupsByID[group.ID] = group
	snapshot.groupsByPlatform[group.Platform] = append(snapshot.groupsByPlatform[group.Platform], group)
	sortPlatformGroups(snapshot.groupsByPlatform[group.Platform])
}

func removeGroupNode(snapshot *Snapshot, groupID int) {
	if snapshot == nil {
		return
	}
	old := snapshot.groupsByID[groupID]
	if old == nil {
		return
	}
	delete(snapshot.groupsByID, groupID)
	groups := snapshot.groupsByPlatform[old.Platform]
	for i, group := range groups {
		if group.ID == groupID {
			groups = append(groups[:i], groups[i+1:]...)
			break
		}
	}
	if len(groups) == 0 {
		delete(snapshot.groupsByPlatform, old.Platform)
	} else {
		snapshot.groupsByPlatform[old.Platform] = groups
	}
}

func putAccountNode(snapshot *Snapshot, account *AccountNode) {
	if snapshot == nil || account == nil || account.Account == nil {
		return
	}
	groupIDs := accountGroupIDSet(account.Account)
	changedGroups := make([]*GroupNode, 0, len(groupIDs)+1)
	for groupID, group := range snapshot.groupsByID {
		_, linked := groupIDs[groupID]
		if linked && group.Platform != account.Account.Platform {
			linked = false
		}
		accounts, changed := replaceAccountNode(group.Accounts, account, linked)
		if changed {
			changedGroups = append(changedGroups, withAccountNodes(group, accounts))
		}
	}
	for _, group := range changedGroups {
		putGroupNode(snapshot, group)
	}
}

func removeAccountNode(snapshot *Snapshot, accountID int) {
	if snapshot == nil || accountID <= 0 {
		return
	}
	var changedGroups []*GroupNode
	for _, group := range snapshot.groupsByID {
		accounts, changed := removeAccountFromNodes(group.Accounts, accountID)
		if changed {
			changedGroups = append(changedGroups, withAccountNodes(group, accounts))
		}
	}
	for _, group := range changedGroups {
		putGroupNode(snapshot, group)
	}
}

func replaceAccountNode(accounts []*AccountNode, account *AccountNode, include bool) ([]*AccountNode, bool) {
	for i, existing := range accounts {
		if existing == nil || existing.Account == nil || existing.Account.ID != account.Account.ID {
			continue
		}
		if !include {
			next := append([]*AccountNode(nil), accounts[:i]...)
			next = append(next, accounts[i+1:]...)
			return next, true
		}
		next := append([]*AccountNode(nil), accounts...)
		next[i] = account
		return next, true
	}
	if !include {
		return accounts, false
	}
	next := append([]*AccountNode(nil), accounts...)
	next = append(next, account)
	return next, true
}

func removeAccountFromNodes(accounts []*AccountNode, accountID int) ([]*AccountNode, bool) {
	for i, existing := range accounts {
		if existing == nil || existing.Account == nil || existing.Account.ID != accountID {
			continue
		}
		next := append([]*AccountNode(nil), accounts[:i]...)
		next = append(next, accounts[i+1:]...)
		return next, true
	}
	return accounts, false
}

func accountGroupIDSet(account *ent.Account) map[int]struct{} {
	groupIDs := make(map[int]struct{}, len(account.Edges.Groups))
	for _, group := range account.Edges.Groups {
		groupIDs[group.ID] = struct{}{}
	}
	return groupIDs
}

func replaceAllowedUser(snapshot *Snapshot, userID int, allowedGroupIDs map[int]struct{}) {
	var changedGroups []*GroupNode
	for _, group := range snapshot.groupsByID {
		_, shouldAllow := allowedGroupIDs[group.ID]
		_, allowed := group.AllowedUsers[userID]
		if shouldAllow == allowed {
			continue
		}
		next := *group
		next.AllowedUsers = cloneAllowedUsers(group.AllowedUsers)
		if shouldAllow {
			next.AllowedUsers[userID] = struct{}{}
		} else {
			delete(next.AllowedUsers, userID)
		}
		changedGroups = append(changedGroups, &next)
	}
	for _, group := range changedGroups {
		putGroupNode(snapshot, group)
	}
}

func sortPlatformGroups(groups []*GroupNode) {
	sort.Slice(groups, func(i, j int) bool {
		a := groups[i]
		b := groups[j]
		if a.SortWeight != b.SortWeight {
			return a.SortWeight > b.SortWeight
		}
		return a.ID < b.ID
	})
}

func accountCategoryKeys(account *ent.Account) []string {
	if account == nil {
		return nil
	}
	seen := make(map[string]struct{}, 8)
	var keys []string
	typeKey := normalizeCategory(account.Type)
	addNormalized := func(value string) {
		key := normalizeCategory(value)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	addCategoryValue := func(value string) {
		addNormalized(value)
		for _, alias := range accountCategoryAliases(value) {
			addNormalized(alias)
		}
	}
	if typeKey != "oauth" {
		addNormalized(account.Type)
	}
	for _, key := range categoryCredentialKeys() {
		addCategoryValue(account.Credentials[key])
		addCategoryValue(extraString(account.Extra, key))
	}
	if typeKey == "oauth" && !hasNonDefaultOAuthCategory(keys) {
		addNormalized(account.Type)
	}
	return keys
}

func hasNonDefaultOAuthCategory(keys []string) bool {
	for _, key := range keys {
		if key != "" && key != "oauth" {
			return true
		}
	}
	return false
}

func categoryCredentialKeys() []string {
	return []string{
		"plan_type",
		"plan",
		"account_type",
		"account_category",
		"subscription_type",
	}
}

func normalizeCategory(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func accountCategoryAliases(value string) []string {
	tokens := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	if len(tokens) == 0 {
		return nil
	}
	aliases := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if values, ok := accountCategoryTokenAliases[token]; ok {
			aliases = append(aliases, values...)
		}
		if _, ok := knownAccountCategoryAliases[token]; ok {
			aliases = append(aliases, token)
		}
	}
	return aliases
}

func extraString(extra map[string]interface{}, key string) string {
	if len(extra) == 0 {
		return ""
	}
	value, ok := extra[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

func cloneAllowedUsers(input map[int]struct{}) map[int]struct{} {
	cloned := make(map[int]struct{}, len(input))
	for key := range input {
		cloned[key] = struct{}{}
	}
	return cloned
}

func cloneGroupRates(input map[int64]float64) map[int64]float64 {
	if input == nil {
		return nil
	}
	cloned := make(map[int64]float64, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneModelRouting(input map[string][]int64) map[string][]int64 {
	if input == nil {
		return nil
	}
	cloned := make(map[string][]int64, len(input))
	for key, value := range input {
		cloned[key] = append([]int64(nil), value...)
	}
	return cloned
}

func cloneDispatchDSL(input sdk.DispatchDSL) sdk.DispatchDSL {
	if len(input.Rules) == 0 {
		return sdk.DispatchDSL{}
	}
	cloned := sdk.DispatchDSL{Rules: make([]sdk.DispatchRule, 0, len(input.Rules))}
	for _, rule := range input.Rules {
		next := rule
		if len(rule.Candidates) > 0 {
			next.Candidates = append([]sdk.DispatchCandidate(nil), rule.Candidates...)
		}
		if len(rule.When.Methods) > 0 {
			next.When.Methods = append([]string(nil), rule.When.Methods...)
		}
		if len(rule.When.Paths) > 0 {
			next.When.Paths = append([]string(nil), rule.When.Paths...)
		}
		if len(rule.When.PathPrefixes) > 0 {
			next.When.PathPrefixes = append([]string(nil), rule.When.PathPrefixes...)
		}
		if len(rule.When.Models) > 0 {
			next.When.Models = append([]string(nil), rule.When.Models...)
		}
		if len(rule.When.ModelPrefixes) > 0 {
			next.When.ModelPrefixes = append([]string(nil), rule.When.ModelPrefixes...)
		}
		if len(rule.When.ModelSuffixes) > 0 {
			next.When.ModelSuffixes = append([]string(nil), rule.When.ModelSuffixes...)
		}
		cloned.Rules = append(cloned.Rules, next)
	}
	return cloned
}

func cloneOperationPolicies(input map[string]bool) map[string]bool {
	if input == nil {
		return nil
	}
	cloned := make(map[string]bool, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func clonePluginSettings(input map[string]map[string]string) map[string]map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]map[string]string, len(input))
	for plugin, settings := range input {
		if len(settings) == 0 {
			continue
		}
		next := make(map[string]string, len(settings))
		for key, value := range settings {
			next[key] = value
		}
		cloned[plugin] = next
	}
	return cloned
}

func matchModelRouting(routing map[string][]int64, model string) []int64 {
	if ids, ok := routing[model]; ok {
		return ids
	}
	for pattern, ids := range routing {
		if matched, _ := filepath.Match(pattern, model); matched {
			return ids
		}
	}
	return nil
}

func groupDispatchResolverCacheKey(groupID int, updatedAt time.Time) string {
	return "group:" + strconv.Itoa(groupID) + ":" + strconv.FormatInt(updatedAt.UnixNano(), 10)
}
