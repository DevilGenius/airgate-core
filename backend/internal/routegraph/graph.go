package routegraph

import (
	"context"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	entuser "github.com/DevilGenius/airgate-core/ent/user"
	"github.com/DevilGenius/airgate-core/internal/dispatchresolver"
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
	ID                int
	Name              string
	Platform          string
	RateMultiplier    float64
	IsExclusive       bool
	AllowedUsers      map[int]struct{}
	ModelRouting      map[string][]int64
	DispatchDSL       sdk.DispatchDSL
	DispatchResolver  *dispatchresolver.CompiledResolver
	OperationPolicies map[string]bool
	PluginSettings    map[string]map[string]string
	ServiceTier       string
	ForceInstructions string
	SortWeight        int
	UpdatedAt         time.Time
	Accounts          []*ent.Account
}

type UserNode struct {
	ID             int
	Email          string
	Balance        float64
	GroupRates     map[int64]float64
	MaxConcurrency int
}

type APIKeyNode struct {
	ID       int
	UserID   int
	SellRate float64
}

var snapshotValue atomic.Value // *Snapshot

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
			q.WithProxy()
		}).
		All(ctx)
	if err != nil {
		return err
	}

	users, err := db.User.Query().All(ctx)
	if err != nil {
		return err
	}

	apiKeys, err := db.APIKey.Query().WithUser().All(ctx)
	if err != nil {
		return err
	}

	next := &Snapshot{
		groupsByID:       make(map[int]*GroupNode, len(groups)),
		groupsByPlatform: make(map[string][]*GroupNode),
		usersByID:        make(map[int]*UserNode, len(users)),
		apiKeysByID:      make(map[int]*APIKeyNode, len(apiKeys)),
		refreshedAt:      time.Now(),
	}

	for _, user := range users {
		next.usersByID[user.ID] = &UserNode{
			ID:             user.ID,
			Email:          user.Email,
			Balance:        user.Balance,
			GroupRates:     cloneGroupRates(user.GroupRates),
			MaxConcurrency: user.MaxConcurrency,
		}
	}

	for _, key := range apiKeys {
		userID := 0
		if key.Edges.User != nil {
			userID = key.Edges.User.ID
		}
		next.apiKeysByID[key.ID] = &APIKeyNode{
			ID:       key.ID,
			UserID:   userID,
			SellRate: key.SellRate,
		}
	}

	for _, group := range groups {
		node := &GroupNode{
			ID:                group.ID,
			Name:              group.Name,
			Platform:          group.Platform,
			RateMultiplier:    group.RateMultiplier,
			IsExclusive:       group.IsExclusive,
			AllowedUsers:      make(map[int]struct{}, len(group.Edges.AllowedUsers)),
			ModelRouting:      cloneModelRouting(group.ModelRouting),
			DispatchDSL:       cloneDispatchDSL(group.DispatchDsl),
			OperationPolicies: cloneOperationPolicies(group.OperationPolicies),
			PluginSettings:    clonePluginSettings(group.PluginSettings),
			ServiceTier:       group.ServiceTier,
			ForceInstructions: group.ForceInstructions,
			SortWeight:        group.SortWeight,
			UpdatedAt:         group.UpdatedAt,
			Accounts:          append([]*ent.Account(nil), group.Edges.Accounts...),
		}
		node.DispatchResolver = dispatchresolver.CompileCached(groupDispatchResolverCacheKey(group.ID, group.UpdatedAt), node.DispatchDSL)
		for _, allowed := range group.Edges.AllowedUsers {
			node.AllowedUsers[allowed.ID] = struct{}{}
		}
		next.groupsByID[node.ID] = node
		next.groupsByPlatform[node.Platform] = append(next.groupsByPlatform[node.Platform], node)
	}

	for platform := range next.groupsByPlatform {
		sort.Slice(next.groupsByPlatform[platform], func(i, j int) bool {
			a := next.groupsByPlatform[platform][i]
			b := next.groupsByPlatform[platform][j]
			if a.SortWeight != b.SortWeight {
				return a.SortWeight > b.SortWeight
			}
			return a.ID < b.ID
		})
	}

	snapshotValue.Store(next)
	return nil
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

func groupDispatchResolverCacheKey(groupID int, updatedAt time.Time) string {
	return "group:" + strconv.Itoa(groupID) + ":" + strconv.FormatInt(updatedAt.UnixNano(), 10)
}
