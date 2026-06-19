package routing

import (
	"context"
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/migrate"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestListEligibleGroups(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "route_selector", migrate.WithGlobalUniqueID(false))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})

	u := db.User.Create().
		SetEmail("user@example.com").
		SetPasswordHash("hash").
		SetGroupRates(map[int64]float64{}).
		SaveX(ctx)

	publicSlow := db.Group.Create().
		SetName("public slow").
		SetPlatform("openai").
		SetRateMultiplier(0.8).
		SetSortWeight(10).
		SaveX(ctx)
	allowedFast := db.Group.Create().
		SetName("allowed fast").
		SetPlatform("openai").
		SetRateMultiplier(0.4).
		SetIsExclusive(true).
		SetSortWeight(1).
		AddAllowedUsers(u).
		SaveX(ctx)
	db.Group.Create().
		SetName("denied fast").
		SetPlatform("openai").
		SetRateMultiplier(0.1).
		SetIsExclusive(true).
		SaveX(ctx)
	tieHighWeight := db.Group.Create().
		SetName("tie high weight").
		SetPlatform("openai").
		SetRateMultiplier(0.8).
		SetSortWeight(20).
		SaveX(ctx)
	db.Group.Create().
		SetName("other platform").
		SetPlatform("anthropic").
		SetRateMultiplier(0.01).
		SaveX(ctx)
	addTestAccountToGroups(ctx, db, publicSlow.ID, allowedFast.ID, tieHighWeight.ID)
	if err := routegraph.RefreshSync(ctx, db); err != nil {
		t.Fatal(err)
	}

	routes, err := ListEligibleGroups(ctx, db, u.ID, "openai", map[int64]float64{int64(publicSlow.ID): 0.3}, RequestInput{
		Method:      "POST",
		Path:        "/v1/chat/completions",
		ClientModel: "gpt-5.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 {
		t.Fatalf("len(routes) = %d, want 3", len(routes))
	}

	wantIDs := []int{publicSlow.ID, allowedFast.ID, tieHighWeight.ID}
	for i, want := range wantIDs {
		if routes[i].GroupID != want {
			t.Fatalf("routes[%d].GroupID = %d, want %d", i, routes[i].GroupID, want)
		}
	}
	if routes[0].EffectiveRate != 0.3 {
		t.Fatalf("routes[0].EffectiveRate = %v, want 0.3", routes[0].EffectiveRate)
	}
}

func TestListEligibleGroupsFiltersOperationDisabledGroups(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "route_selector_image", migrate.WithGlobalUniqueID(false))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})

	u := db.User.Create().
		SetEmail("image-user@example.com").
		SetPasswordHash("hash").
		SetGroupRates(map[int64]float64{}).
		SaveX(ctx)

	imageDispatchDSL := sdk.DispatchDSL{
		Rules: []sdk.DispatchRule{{
			ID:        "images-generate",
			Operation: "images.generate",
			When: sdk.DispatchWhen{
				Methods: []string{"POST"},
				Paths:   []string{"/v1/images/generations"},
			},
			Gate: sdk.DispatchGate{RequiredOperation: "images.generate"},
			Candidates: []sdk.DispatchCandidate{
				{Scheduling: "${model}", Wire: "${model}"},
			},
		}},
	}

	db.Group.Create().
		SetName("image disabled").
		SetPlatform("openai").
		SetRateMultiplier(0.1).
		SetDispatchDsl(imageDispatchDSL).
		SaveX(ctx)
	imageEnabled := db.Group.Create().
		SetName("image enabled").
		SetPlatform("openai").
		SetRateMultiplier(0.2).
		SetOperationPolicies(map[string]bool{"images.generate": true}).
		SetDispatchDsl(imageDispatchDSL).
		SaveX(ctx)
	db.Group.Create().
		SetName("chat only implicit").
		SetPlatform("openai").
		SetRateMultiplier(0.3).
		SetDispatchDsl(imageDispatchDSL).
		SaveX(ctx)
	openaiGroups := db.Group.Query().AllX(ctx)
	groupIDs := make([]int, 0, len(openaiGroups))
	for _, group := range openaiGroups {
		if group.Platform == "openai" {
			groupIDs = append(groupIDs, group.ID)
		}
	}
	addTestAccountToGroups(ctx, db, groupIDs...)
	if err := routegraph.RefreshSync(ctx, db); err != nil {
		t.Fatal(err)
	}

	routes, err := ListEligibleGroups(ctx, db, u.ID, "openai", nil, RequestInput{
		Method:      "POST",
		Path:        "/v1/images/generations",
		ClientModel: "gpt-5.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	if routes[0].GroupID != imageEnabled.ID {
		t.Fatalf("routes[0].GroupID = %d, want %d", routes[0].GroupID, imageEnabled.ID)
	}

	chatRoutes, err := ListEligibleGroups(ctx, db, u.ID, "openai", nil, RequestInput{
		Method:      "POST",
		Path:        "/v1/chat/completions",
		ClientModel: "gpt-5.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chatRoutes) != 3 {
		t.Fatalf("len(chatRoutes) = %d, want 3", len(chatRoutes))
	}
}

func addTestAccountToGroups(ctx context.Context, db *ent.Client, groupIDs ...int) {
	db.Account.Create().
		SetName("route account").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SetPriority(50).
		SetMaxConcurrency(10).
		AddGroupIDs(groupIDs...).
		SaveX(ctx)
}
