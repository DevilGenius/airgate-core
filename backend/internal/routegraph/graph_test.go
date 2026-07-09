package routegraph

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/migrate"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestAccountsForModelAppliesModelPolicies(t *testing.T) {
	plus := &ent.Account{
		ID:          1,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "ChatGPT Plus"},
		ModelPolicy: modelpolicy.Policy{Deny: []string{"gpt-4o"}},
	}
	pro := &ent.Account{
		ID:          2,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "Builder Id Pro"},
		Extra:       map[string]interface{}{},
	}
	professional := &ent.Account{
		ID:          4,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "Professional"},
		Extra:       map[string]interface{}{},
	}
	apiKey := &ent.Account{
		ID:       3,
		Platform: "openai",
		Type:     "apikey",
		Extra:    map[string]interface{}{},
	}
	group := &ent.Group{
		ID:          10,
		Platform:    "openai",
		ModelPolicy: modelpolicy.Policy{Deny: []string{"blocked-*"}},
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{
			"plus": {Deny: []string{"gpt-5*"}},
			"pro":  {Allow: []string{"gpt-5*"}},
		},
	}
	group.Edges.Accounts = []*ent.Account{plus, pro, apiKey, professional}
	restore := SetSnapshotForTesting([]*ent.Group{group})
	defer restore()

	node := Group(group.ID)
	if got := accountIDs(node.AccountsForModel("blocked-model")); len(got) != 0 {
		t.Fatalf("blocked model accounts = %v, want none", got)
	}
	if got := accountIDs(node.AccountsForModel("gpt-5.1")); !sameIDs(got, []int{2, 3, 4}) {
		t.Fatalf("gpt-5 accounts = %v, want [2 3 4]", got)
	}
	if got := accountIDs(node.AccountsForModel("gpt-4o")); !sameIDs(got, []int{3, 4}) {
		t.Fatalf("gpt-4o accounts = %v, want [3 4]", got)
	}
}

func TestAccountsForModelOAuthDefaultPolicyOnlyMatchesDefaultOAuth(t *testing.T) {
	defaultOAuth := &ent.Account{ID: 1, Platform: "openai", Type: "oauth"}
	teamOAuth := &ent.Account{
		ID:          2,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "k12"},
	}
	apiKey := &ent.Account{ID: 3, Platform: "openai", Type: "apikey"}
	group := &ent.Group{
		ID:       11,
		Platform: "openai",
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{
			"oauth": {Deny: []string{"default-blocked"}},
			"team":  {Deny: []string{"team-blocked"}},
		},
	}
	group.Edges.Accounts = []*ent.Account{defaultOAuth, teamOAuth, apiKey}
	restore := SetSnapshotForTesting([]*ent.Group{group})
	defer restore()

	node := Group(group.ID)
	if got := accountIDs(node.AccountsForModel("default-blocked")); !sameIDs(got, []int{2, 3}) {
		t.Fatalf("default-blocked accounts = %v, want [2 3]", got)
	}
	if got := accountIDs(node.AccountsForModel("team-blocked")); !sameIDs(got, []int{1, 3}) {
		t.Fatalf("team-blocked accounts = %v, want [1 3]", got)
	}
}

func TestAccountsForModelAppliesGroupPriorityOverrides(t *testing.T) {
	shared := &ent.Account{
		ID:       1,
		Platform: "openai",
		Priority: 50,
		Extra: map[string]interface{}{
			accountGroupPrioritiesExtraKey: map[string]interface{}{
				"10": float64(80),
				"20": float64(-5),
			},
		},
	}
	fallback := &ent.Account{
		ID:       2,
		Platform: "openai",
		Priority: 30,
		Extra:    map[string]interface{}{},
	}
	groupA := &ent.Group{ID: 10, Platform: "openai"}
	groupA.Edges.Accounts = []*ent.Account{shared, fallback}
	groupB := &ent.Group{ID: 20, Platform: "openai"}
	groupB.Edges.Accounts = []*ent.Account{shared, fallback}
	groupC := &ent.Group{ID: 30, Platform: "openai"}
	groupC.Edges.Accounts = []*ent.Account{shared, fallback}

	restore := SetSnapshotForTesting([]*ent.Group{groupA, groupB, groupC})
	defer restore()

	if got := accountPriorities(Group(10).AccountsForModel("gpt-5")); got[1] != 80 || got[2] != 30 {
		t.Fatalf("group 10 priorities = %+v, want account 1 override and account 2 fallback", got)
	}
	if got := accountPriorities(Group(20).AccountsForModel("gpt-5")); got[1] != -5 || got[2] != 30 {
		t.Fatalf("group 20 priorities = %+v, want account 1 override and account 2 fallback", got)
	}
	if got := accountPriorities(Group(30).AccountsForModel("gpt-5")); got[1] != 50 || got[2] != 30 {
		t.Fatalf("group 30 priorities = %+v, want account-level fallback", got)
	}
	if shared.Priority != 50 {
		t.Fatalf("source account priority mutated to %d", shared.Priority)
	}
}

func TestRefreshSyncAndIncrementalUpdates(t *testing.T) {
	restore := preserveSnapshot(t)
	defer restore()

	db := openRouteGraphDB(t, "routegraph_refresh")
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user, err := db.User.Create().
		SetEmail("routegraph@example.com").
		SetPasswordHash("hash").
		SetBalance(12.5).
		SetMaxConcurrency(7).
		SetGroupRates(map[int64]float64{100: 1.5}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := db.Group.Create().
		SetName("OpenAI Primary").
		SetPlatform("openai").
		SetRateMultiplier(2).
		SetIsExclusive(true).
		SetSortWeight(10).
		AddAllowedUserIDs(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("OpenAI Account").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"plan_type": "ChatGPT Plus"}).
		AddGroupIDs(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("Claude Account").
		SetPlatform("claude").
		SetType("oauth").
		SetCredentials(map[string]string{"plan_type": "Pro"}).
		AddGroupIDs(group.ID).
		Save(ctx); err != nil {
		t.Fatalf("create mismatched account: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("route-key").
		SetKeyHash("hash-route-key").
		SetSellRate(3).
		SetUserID(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if err := RefreshSync(ctx, nil); err != nil {
		t.Fatalf("RefreshSync nil db returned error: %v", err)
	}
	if err := RefreshSync(ctx, db); err != nil {
		t.Fatalf("RefreshSync returned error: %v", err)
	}

	groupNode := Group(group.ID)
	if groupNode == nil || groupNode.Name != "OpenAI Primary" || len(groupNode.Accounts) != 1 || groupNode.Accounts[0].Account.ID != account.ID {
		t.Fatalf("refreshed group = %+v", groupNode)
	}
	if _, ok := groupNode.AllowedUsers[user.ID]; !ok {
		t.Fatalf("allowed users = %+v, want user %d", groupNode.AllowedUsers, user.ID)
	}
	if groups := GroupsByPlatform("openai"); len(groups) != 1 || groups[0].ID != group.ID {
		t.Fatalf("GroupsByPlatform = %+v, want refreshed group", groups)
	}
	if userNode := User(user.ID); userNode == nil || userNode.Email != user.Email || userNode.Balance != 12.5 || userNode.MaxConcurrency != 7 {
		t.Fatalf("User = %+v", userNode)
	}
	if userNode := User(user.ID); !userNode.Active() {
		t.Fatalf("User active = false: %+v", userNode)
	}
	if keyNode := APIKey(key.ID); keyNode == nil || keyNode.UserID != user.ID || keyNode.SellRate != 3 || !keyNode.Active(time.Now()) {
		t.Fatalf("APIKey = %+v", keyNode)
	}

	addedAccount, err := db.Account.Create().
		SetName("Added").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-added"}).
		AddGroupIDs(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create added account: %v", err)
	}
	if err := RefreshAccount(ctx, db, addedAccount.ID); err != nil {
		t.Fatalf("RefreshAccount add returned error: %v", err)
	}
	if got := accountIDs(Group(group.ID).AccountsForModel("anything")); !sameIDs(got, []int{account.ID, addedAccount.ID}) {
		t.Fatalf("accounts after add = %v", got)
	}

	if err := db.Account.UpdateOneID(addedAccount.ID).SetPlatform("claude").Exec(ctx); err != nil {
		t.Fatalf("update account platform: %v", err)
	}
	if err := RefreshAccount(ctx, db, addedAccount.ID); err != nil {
		t.Fatalf("RefreshAccount platform change returned error: %v", err)
	}
	if got := accountIDs(Group(group.ID).AccountsForModel("anything")); !sameIDs(got, []int{account.ID}) {
		t.Fatalf("accounts after platform mismatch = %v", got)
	}
	if err := db.Account.DeleteOneID(addedAccount.ID).Exec(ctx); err != nil {
		t.Fatalf("delete added account: %v", err)
	}
	if err := RefreshAccount(ctx, db, addedAccount.ID); err != nil {
		t.Fatalf("RefreshAccount deleted returned error: %v", err)
	}

	newGroup, err := db.Group.Create().
		SetName("New Exclusive").
		SetPlatform("openai").
		SetIsExclusive(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create new group: %v", err)
	}
	if err := db.User.UpdateOneID(user.ID).AddAllowedGroupIDs(newGroup.ID).Exec(ctx); err != nil {
		t.Fatalf("update user allowed groups: %v", err)
	}
	if err := RefreshGroup(ctx, db, newGroup.ID); err != nil {
		t.Fatalf("RefreshGroup new returned error: %v", err)
	}
	if err := RefreshUser(ctx, db, user.ID); err != nil {
		t.Fatalf("RefreshUser returned error: %v", err)
	}
	if _, ok := Group(newGroup.ID).AllowedUsers[user.ID]; !ok {
		t.Fatalf("new group allowed users = %+v", Group(newGroup.ID).AllowedUsers)
	}

	if err := db.APIKey.UpdateOneID(key.ID).SetSellRate(4).Exec(ctx); err != nil {
		t.Fatalf("update api key: %v", err)
	}
	if err := RefreshAPIKey(ctx, db, key.ID); err != nil {
		t.Fatalf("RefreshAPIKey returned error: %v", err)
	}
	if got := APIKey(key.ID).SellRate; got != 4 {
		t.Fatalf("APIKey sell rate = %v, want 4", got)
	}
	if err := db.APIKey.UpdateOneID(key.ID).SetQuotaUsd(1).SetUsedQuota(1).Exec(ctx); err != nil {
		t.Fatalf("exhaust api key: %v", err)
	}
	if err := RefreshAPIKey(ctx, db, key.ID); err != nil {
		t.Fatalf("RefreshAPIKey exhausted returned error: %v", err)
	}
	if keyNode := APIKey(key.ID); keyNode == nil || !keyNode.QuotaExhausted() || keyNode.Active(time.Now()) {
		t.Fatalf("exhausted APIKey = %+v", keyNode)
	}
	if reason := APIKey(key.ID).InactiveReason(time.Now()); reason != APIKeyInactiveExhausted {
		t.Fatalf("exhausted APIKey inactive reason = %q, want %q", reason, APIKeyInactiveExhausted)
	}
	if err := db.APIKey.DeleteOneID(key.ID).Exec(ctx); err != nil {
		t.Fatalf("delete api key: %v", err)
	}
	if err := RefreshAPIKey(ctx, db, key.ID); err != nil {
		t.Fatalf("RefreshAPIKey deleted returned error: %v", err)
	}
	if APIKey(key.ID) != nil {
		t.Fatal("deleted API key still present")
	}

	if err := db.Group.DeleteOneID(newGroup.ID).Exec(ctx); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if err := RefreshGroup(ctx, db, newGroup.ID); err != nil {
		t.Fatalf("RefreshGroup deleted returned error: %v", err)
	}
	if Group(newGroup.ID) != nil {
		t.Fatal("deleted group still present")
	}

	if err := db.User.DeleteOneID(user.ID).Exec(ctx); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if err := RefreshUser(ctx, db, user.ID); err != nil {
		t.Fatalf("RefreshUser deleted returned error: %v", err)
	}
	if User(user.ID) != nil {
		t.Fatal("deleted user still present")
	}
}

func TestRefreshAndRemoveInvalidInputs(t *testing.T) {
	restore := preserveSnapshot(t)
	defer restore()

	if err := RefreshGroup(context.Background(), nil, 1); err != nil {
		t.Fatalf("RefreshGroup nil db returned error: %v", err)
	}
	if err := RefreshGroup(context.Background(), nil, 0); err != nil {
		t.Fatalf("RefreshGroup invalid ID returned error: %v", err)
	}
	if err := RefreshAccount(context.Background(), nil, 1); err != nil {
		t.Fatalf("RefreshAccount nil db returned error: %v", err)
	}
	if err := RefreshAccount(context.Background(), nil, 0); err != nil {
		t.Fatalf("RefreshAccount invalid ID returned error: %v", err)
	}
	if err := RefreshUser(context.Background(), nil, 1); err != nil {
		t.Fatalf("RefreshUser nil db returned error: %v", err)
	}
	if err := RefreshUser(context.Background(), nil, 0); err != nil {
		t.Fatalf("RefreshUser invalid ID returned error: %v", err)
	}
	if err := RefreshAPIKey(context.Background(), nil, 1); err != nil {
		t.Fatalf("RefreshAPIKey nil db returned error: %v", err)
	}
	if err := RefreshAPIKey(context.Background(), nil, 0); err != nil {
		t.Fatalf("RefreshAPIKey invalid ID returned error: %v", err)
	}
	RemoveGroup(0)
	RemoveAccount(0)
	RemoveUser(0)
	RemoveAPIKey(0)
}

func TestRefreshReturnsDatabaseErrors(t *testing.T) {
	restore := preserveSnapshot(t)
	defer restore()

	ctx := context.Background()
	db := openRouteGraphDB(t, "routegraph_closed")
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if err := RefreshSync(ctx, db); err == nil {
		t.Fatal("RefreshSync on closed db returned nil error")
	}
	if err := RefreshGroup(ctx, db, 1); err == nil {
		t.Fatal("RefreshGroup on closed db returned nil error")
	}
	if err := RefreshAccount(ctx, db, 1); err == nil {
		t.Fatal("RefreshAccount on closed db returned nil error")
	}
	if err := RefreshUser(ctx, db, 1); err == nil {
		t.Fatal("RefreshUser on closed db returned nil error")
	}
	if err := RefreshAPIKey(ctx, db, 1); err == nil {
		t.Fatal("RefreshAPIKey on closed db returned nil error")
	}
}

func TestRefreshSyncReturnsUserAndAPIKeyQueryErrors(t *testing.T) {
	restore := preserveSnapshot(t)
	defer restore()

	db := openRouteGraphDB(t, "routegraph_refresh_query_errors")
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()

	userErr := errors.New("users failed")
	oldUsers := queryRefreshUsers
	queryRefreshUsers = func(context.Context, *ent.Client) ([]*ent.User, error) {
		return nil, userErr
	}
	if err := RefreshSync(ctx, db); !errors.Is(err, userErr) {
		t.Fatalf("RefreshSync user error = %v, want %v", err, userErr)
	}
	queryRefreshUsers = oldUsers

	keyErr := errors.New("api keys failed")
	oldAPIKeys := queryRefreshAPIKeys
	queryRefreshAPIKeys = func(context.Context, *ent.Client) ([]*ent.APIKey, error) {
		return nil, keyErr
	}
	if err := RefreshSync(ctx, db); !errors.Is(err, keyErr) {
		t.Fatalf("RefreshSync api key error = %v, want %v", err, keyErr)
	}
	queryRefreshAPIKeys = oldAPIKeys
}

func TestNilSnapshotGetters(t *testing.T) {
	updateMu.Lock()
	previous := Current()
	snapshotValue = atomic.Value{}
	updateMu.Unlock()
	defer func() {
		updateMu.Lock()
		defer updateMu.Unlock()
		if previous == nil {
			snapshotValue.Store(newEmptySnapshot())
			return
		}
		snapshotValue.Store(previous)
	}()

	if Current() != nil || Group(1) != nil || GroupsByPlatform("openai") != nil || User(1) != nil || APIKey(1) != nil {
		t.Fatal("nil snapshot getters should return nil")
	}
}

func TestHasAccountForModelAndRouting(t *testing.T) {
	one := &ent.Account{ID: 1, Platform: "openai", Type: "apikey", Extra: map[string]interface{}{}}
	two := &ent.Account{ID: 2, Platform: "openai", Type: "oauth", Credentials: map[string]string{"plan_type": "Pro"}, Extra: map[string]interface{}{}}
	three := &ent.Account{ID: 3, Platform: "openai", Type: "oauth", Credentials: map[string]string{"plan_type": "Free"}, Extra: map[string]interface{}{}}
	group := &ent.Group{
		ID:           99,
		Platform:     "openai",
		ModelRouting: map[string][]int64{"gpt-5": {1}, "o3-*": {2}},
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{
			"free": {Deny: []string{"*"}},
		},
	}
	group.Edges.Accounts = []*ent.Account{one, two, three}
	restore := SetSnapshotForTesting([]*ent.Group{group})
	defer restore()

	node := Group(group.ID)
	if got := accountIDs(node.AccountsForModel("gpt-5")); !sameIDs(got, []int{1}) {
		t.Fatalf("exact routed accounts = %v, want [1]", got)
	}
	if got := node.AccountsForModel("unknown"); got != nil {
		t.Fatalf("unmatched routed accounts = %+v, want nil", got)
	}
	if !node.HasAccountForModel("o3-mini") {
		t.Fatal("HasAccountForModel(o3-mini) = false, want true")
	}
	if node.HasAccountForModel("unknown") {
		t.Fatal("HasAccountForModel(unknown) = true, want false")
	}
	var nilGroup *GroupNode
	if nilGroup.AccountsForModel("gpt-5") != nil || nilGroup.HasAccountForModel("gpt-5") {
		t.Fatal("nil group should not return accounts")
	}
	routedWithNilNodes := withAccountNodes(&GroupNode{
		ModelRouting: map[string][]int64{"gpt-5": {20}},
		modelPolicy:  modelpolicy.Compile(modelpolicy.Policy{}),
	}, []*AccountNode{nil, {}, {Account: &ent.Account{ID: 20}}})
	if got := accountIDs(routedWithNilNodes.AccountsForModel("gpt-5")); !sameIDs(got, []int{20}) {
		t.Fatalf("routed accounts with nil nodes = %v, want [20]", got)
	}

	unrestricted := withAccountNodes(&GroupNode{}, []*AccountNode{{Account: &ent.Account{ID: 10}}})
	if !unrestricted.HasAccountForModel("anything") {
		t.Fatal("unrestricted group should have account")
	}
	restricted := withAccountNodes(&GroupNode{
		ModelPolicy: modelpolicy.Policy{Deny: []string{"blocked"}},
		modelPolicy: modelpolicy.Compile(modelpolicy.Policy{Deny: []string{"blocked"}}),
	}, []*AccountNode{{Account: &ent.Account{ID: 11}}})
	if restricted.HasAccountForModel("blocked") {
		t.Fatal("group-level denied model should not be available")
	}
	nodeDenied := withAccountNodes(&GroupNode{}, []*AccountNode{
		nil,
		{},
		{Account: &ent.Account{ID: 12}, ModelPolicy: modelpolicy.Policy{Deny: []string{"denied"}}, modelPolicy: modelpolicy.Compile(modelpolicy.Policy{Deny: []string{"denied"}})},
	})
	if nodeDenied.HasAccountForModel("denied") {
		t.Fatal("account-level denied model should not be available")
	}
	typeDenied := withAccountNodes(&GroupNode{
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{"free": {Deny: []string{"*"}}},
		accountTypePolicies:      compileAccountTypePolicies(map[string]modelpolicy.Policy{"free": {Deny: []string{"*"}}}),
	}, []*AccountNode{{Account: &ent.Account{ID: 13}, categoryKeys: []string{"free"}}})
	if typeDenied.HasAccountForModel("anything") {
		t.Fatal("account-type denied model should not be available")
	}
	noPolicyGroup := &GroupNode{}
	if !noPolicyGroup.accountTypeAllows(&AccountNode{}, "anything") {
		t.Fatal("group without account type policies should allow")
	}
	var nilAccountNode *AccountNode
	if nilAccountNode.matchesCategory("pro") || (&AccountNode{}).matchesCategory("pro") {
		t.Fatal("nil or empty account node should not match category")
	}
}

func TestSnapshotAndNodeHelpers(t *testing.T) {
	base := &Snapshot{
		groupsByID:       map[int]*GroupNode{2: {ID: 2, Platform: "openai", SortWeight: 1}},
		groupsByPlatform: map[string][]*GroupNode{"openai": {{ID: 2, Platform: "openai"}}},
		usersByID:        map[int]*UserNode{3: {ID: 3, GroupRates: map[int64]float64{2: 1.2}}},
		apiKeysByID:      map[int]*APIKeyNode{4: {ID: 4, UserID: 3}},
		refreshedAt:      time.Date(2026, 6, 20, 1, 2, 3, 0, time.UTC),
	}
	cloned := cloneSnapshot(base)
	cloned.groupsByPlatform["openai"][0] = &GroupNode{ID: 9, Platform: "openai"}
	if base.groupsByPlatform["openai"][0].ID != 2 {
		t.Fatalf("cloneSnapshot shared platform slice: %+v", base.groupsByPlatform["openai"])
	}
	if empty := cloneSnapshot(nil); empty == nil || len(empty.groupsByID) != 0 || len(empty.usersByID) != 0 {
		t.Fatalf("cloneSnapshot(nil) = %+v", empty)
	}

	snapshot := newEmptySnapshot()
	putGroupNode(nil, &GroupNode{ID: 1})
	putGroupNode(snapshot, nil)
	putGroupNode(snapshot, &GroupNode{ID: 2, Platform: "openai", SortWeight: 1})
	putGroupNode(snapshot, &GroupNode{ID: 1, Platform: "openai", SortWeight: 10})
	putGroupNode(snapshot, &GroupNode{ID: 3, Platform: "openai", SortWeight: 1})
	if got := []int{snapshot.groupsByPlatform["openai"][0].ID, snapshot.groupsByPlatform["openai"][1].ID}; !sameIDs(got, []int{1, 2}) {
		t.Fatalf("sorted groups = %v", got)
	}
	removeGroupNode(nil, 1)
	removeGroupNode(snapshot, 999)
	removeGroupNode(snapshot, 1)
	if snapshot.groupsByID[1] != nil || len(snapshot.groupsByPlatform["openai"]) != 2 {
		t.Fatalf("snapshot after remove = %+v", snapshot)
	}

	account := &AccountNode{Account: &ent.Account{ID: 5, Platform: "openai"}}
	accounts, changed := replaceAccountNode(nil, account, false)
	if changed || accounts != nil {
		t.Fatalf("replace absent excluded = changed %v accounts %+v", changed, accounts)
	}
	accounts, changed = replaceAccountNode(nil, account, true)
	if !changed || len(accounts) != 1 {
		t.Fatalf("replace absent included = changed %v accounts %+v", changed, accounts)
	}
	replacement := &AccountNode{Account: &ent.Account{ID: 5, Platform: "openai", Name: "new"}}
	accounts, changed = replaceAccountNode(accounts, replacement, true)
	if !changed || accounts[0].Account.Name != "new" {
		t.Fatalf("replace existing = changed %v accounts %+v", changed, accounts)
	}
	accounts, changed = replaceAccountNode(accounts, replacement, false)
	if !changed || len(accounts) != 0 {
		t.Fatalf("remove existing = changed %v accounts %+v", changed, accounts)
	}
	accounts, changed = removeAccountFromNodes([]*AccountNode{account}, 6)
	if changed || len(accounts) != 1 {
		t.Fatalf("remove missing account = changed %v accounts %+v", changed, accounts)
	}
	accounts, changed = removeAccountFromNodes([]*AccountNode{account}, 5)
	if !changed || len(accounts) != 0 {
		t.Fatalf("remove account = changed %v accounts %+v", changed, accounts)
	}

	entAccount := &ent.Account{ID: 7}
	entAccount.Edges.Groups = []*ent.Group{{ID: 10}, {ID: 11}}
	if got := accountGroupIDSet(entAccount); len(got) != 2 {
		t.Fatalf("accountGroupIDSet = %+v", got)
	}
	if (&AccountNode{}).matchesCategory("pro") || (&AccountNode{categoryKeys: []string{"pro"}}).matchesCategory(" ") {
		t.Fatal("matchesCategory accepted missing account category")
	}

	putAccountNode(nil, account)
	putAccountNode(snapshot, nil)
	putAccountNode(snapshot, &AccountNode{})
	removeAccountNode(nil, 1)
	removeAccountNode(snapshot, 0)

	groupWithAccount := withAccountNodes(&GroupNode{ID: 20, Platform: "openai"}, []*AccountNode{{Account: &ent.Account{ID: 21, Platform: "openai"}}})
	putGroupNode(snapshot, groupWithAccount)
	removeAccountNode(snapshot, 21)
	if len(snapshot.groupsByID[20].Accounts) != 0 {
		t.Fatalf("removeAccountNode left accounts: %+v", snapshot.groupsByID[20].Accounts)
	}

	allowedSnapshot := newEmptySnapshot()
	putGroupNode(allowedSnapshot, &GroupNode{ID: 30, Platform: "openai", AllowedUsers: map[int]struct{}{}})
	replaceAllowedUser(allowedSnapshot, 44, map[int]struct{}{30: {}})
	if _, ok := allowedSnapshot.groupsByID[30].AllowedUsers[44]; !ok {
		t.Fatalf("replaceAllowedUser did not add user: %+v", allowedSnapshot.groupsByID[30].AllowedUsers)
	}
	replaceAllowedUser(allowedSnapshot, 44, nil)
	if _, ok := allowedSnapshot.groupsByID[30].AllowedUsers[44]; ok {
		t.Fatalf("replaceAllowedUser did not remove user: %+v", allowedSnapshot.groupsByID[30].AllowedUsers)
	}

	removeUserSnapshot := newEmptySnapshot()
	removeUserSnapshot.usersByID[50] = &UserNode{ID: 50}
	removeUserSnapshot.apiKeysByID[60] = &APIKeyNode{ID: 60, UserID: 50}
	snapshotValue.Store(removeUserSnapshot)
	RemoveUser(50)
	if User(50) != nil || APIKey(60) != nil {
		t.Fatalf("RemoveUser left user=%+v key=%+v", User(50), APIKey(60))
	}

	if policies := compileAccountTypePolicies(map[string]modelpolicy.Policy{"open": {}}); len(policies) != 0 {
		t.Fatalf("non-restrictive policies = %+v, want nil", policies)
	}
	groupWithNil := withAccountNodes(&GroupNode{}, []*AccountNode{nil, {}, {Account: &ent.Account{ID: 70}}})
	if len(groupWithNil.accountRefs) != 1 || groupWithNil.accountRefs[0].ID != 70 {
		t.Fatalf("withAccountNodes refs = %+v", groupWithNil.accountRefs)
	}
}

func TestCloneAndCategoryHelpers(t *testing.T) {
	if accountCategoryKeys(nil) != nil {
		t.Fatal("accountCategoryKeys(nil) should return nil")
	}
	account := &ent.Account{
		Type:        "OAuth",
		Credentials: map[string]string{"plan_type": "ChatGPT Plus", "account_category": "Builder-Id Pro", "plan": "k12"},
		Extra:       map[string]interface{}{"subscription_type": "Team", "plan": 42},
	}
	keys := accountCategoryKeys(account)
	for _, want := range []string{"chatgptplus", "plus", "builderidpro", "pro", "k12", "team"} {
		if !containsString(keys, want) {
			t.Fatalf("category keys = %v, missing %q", keys, want)
		}
	}
	if containsString(keys, "oauth") {
		t.Fatalf("category keys = %v, should not include default oauth for typed OAuth account", keys)
	}
	defaultOAuthKeys := accountCategoryKeys(&ent.Account{Type: "OAuth"})
	if !containsString(defaultOAuthKeys, "oauth") {
		t.Fatalf("default OAuth category keys = %v, missing oauth", defaultOAuthKeys)
	}
	if got := extraString(nil, "plan"); got != "" {
		t.Fatalf("extraString nil = %q", got)
	}
	if got := extraString(map[string]interface{}{"plan": 42}, "plan"); got != "" {
		t.Fatalf("extraString non-string = %q", got)
	}
	if got := accountCategoryAliases("plain text"); len(got) != 0 {
		t.Fatalf("aliases without known category = %#v", got)
	}

	groupRates := map[int64]float64{1: 2}
	groupRatesClone := cloneGroupRates(groupRates)
	groupRatesClone[1] = 3
	if groupRates[1] != 2 || cloneGroupRates(nil) != nil {
		t.Fatalf("cloneGroupRates source=%+v clone=%+v", groupRates, groupRatesClone)
	}
	routing := map[string][]int64{"gpt": {1, 2}}
	routingClone := cloneModelRouting(routing)
	routingClone["gpt"][0] = 99
	if routing["gpt"][0] != 1 || cloneModelRouting(nil) != nil {
		t.Fatalf("cloneModelRouting source=%+v clone=%+v", routing, routingClone)
	}
	if got := matchModelRouting(map[string][]int64{"gpt-5": {1}, "o3-*": {2}}, "o3-mini"); !sameInt64s(got, []int64{2}) {
		t.Fatalf("matchModelRouting glob = %v", got)
	}
	if got := matchModelRouting(map[string][]int64{"gpt-5": {1}}, "none"); got != nil {
		t.Fatalf("matchModelRouting missing = %v", got)
	}

	dsl := sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		ID:         "r1",
		Candidates: []sdk.DispatchCandidate{{Scheduling: "gpt", Wire: "wire"}},
		When: sdk.DispatchWhen{
			Methods:       []string{"POST"},
			Paths:         []string{"/v1"},
			PathPrefixes:  []string{"/v1/"},
			Models:        []string{"gpt"},
			ModelPrefixes: []string{"gpt-"},
			ModelSuffixes: []string{"-mini"},
		},
	}}}
	dslClone := cloneDispatchDSL(dsl)
	dslClone.Rules[0].Candidates[0].Scheduling = "mutated"
	dslClone.Rules[0].When.Paths[0] = "/mutated"
	if dsl.Rules[0].Candidates[0].Scheduling != "gpt" || dsl.Rules[0].When.Paths[0] != "/v1" {
		t.Fatalf("cloneDispatchDSL mutated source: %+v", dsl)
	}
	if got := cloneDispatchDSL(sdk.DispatchDSL{}); len(got.Rules) != 0 {
		t.Fatalf("empty dispatch clone = %+v", got)
	}

	ops := map[string]bool{"images.generate": true}
	opsClone := cloneOperationPolicies(ops)
	opsClone["images.generate"] = false
	if !ops["images.generate"] || cloneOperationPolicies(nil) != nil {
		t.Fatalf("cloneOperationPolicies source=%+v clone=%+v", ops, opsClone)
	}
	settings := map[string]map[string]string{"openai": {"mode": "fast"}, "empty": {}}
	settingsClone := clonePluginSettings(settings)
	settingsClone["openai"]["mode"] = "slow"
	_, hasEmptySettings := settingsClone["empty"]
	if settings["openai"]["mode"] != "fast" || hasEmptySettings || clonePluginSettings(nil) != nil {
		t.Fatalf("clonePluginSettings source=%+v clone=%+v", settings, settingsClone)
	}
}

func accountIDs(accounts []*ent.Account) []int {
	out := make([]int, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, account.ID)
	}
	return out
}

func accountPriorities(accounts []*ent.Account) map[int]int {
	out := make(map[int]int, len(accounts))
	for _, account := range accounts {
		out[account.ID] = account.Priority
	}
	return out
}

func sameIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameInt64s(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func openRouteGraphDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	return testdb.OpenMemoryEnt(t, name, migrate.WithGlobalUniqueID(false))
}

func preserveSnapshot(t *testing.T) func() {
	t.Helper()
	updateMu.Lock()
	previous := Current()
	snapshotValue.Store(newEmptySnapshot())
	updateMu.Unlock()
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
