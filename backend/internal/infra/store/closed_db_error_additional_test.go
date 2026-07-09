package store

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
	appgroup "github.com/DevilGenius/airgate-core/internal/app/group"
	appsubscription "github.com/DevilGenius/airgate-core/internal/app/subscription"
	appuser "github.com/DevilGenius/airgate-core/internal/app/user"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestStoresPropagateClosedDBErrors(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "store_closed_db_errors", schema.WithGlobalUniqueID(false))
	accountStore := NewAccountStore(db)
	apiKeyStore := NewAPIKeyStore(db)
	groupStore := NewGroupStore(db)
	subscriptionStore := NewSubscriptionStore(db)
	userStore := NewUserStore(db)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	now := time.Now()
	tests := []struct {
		name string
		call func() error
	}{
		{name: "account list", call: func() error {
			_, _, err := accountStore.List(ctx, appaccount.ListFilter{Page: 1, PageSize: 10})
			return err
		}},
		{name: "account list all", call: func() error {
			_, err := accountStore.ListAll(ctx, appaccount.ListFilter{})
			return err
		}},
		{name: "account list by platform", call: func() error {
			_, err := accountStore.ListByPlatform(ctx, "openai")
			return err
		}},
		{name: "account usage logs", call: func() error {
			_, err := accountStore.FindUsageLogs(ctx, 1, now.Add(-time.Hour), now)
			return err
		}},
		{name: "account window stats", call: func() error {
			_, err := accountStore.BatchWindowStats(ctx, []int{1}, now.Add(-time.Hour))
			return err
		}},
		{name: "account image stats", call: func() error {
			_, err := accountStore.BatchImageStats(ctx, []int{1}, now)
			return err
		}},
		{name: "account update credentials", call: func() error {
			_, err := accountStore.Update(ctx, 1, appaccount.UpdateInput{Credentials: map[string]string{"token": "x"}})
			return err
		}},
		{name: "group list", call: func() error {
			_, _, err := groupStore.List(ctx, appgroup.ListFilter{Page: 1, PageSize: 10})
			return err
		}},
		{name: "group available", call: func() error {
			_, _, err := groupStore.ListAvailable(ctx, appgroup.AvailableFilter{UserID: 1, Page: 1, PageSize: 10})
			return err
		}},
		{name: "group delete", call: func() error {
			return groupStore.Delete(ctx, 1)
		}},
		{name: "group stats", call: func() error {
			_, _, err := groupStore.StatsForGroups(ctx, []int{1}, now)
			return err
		}},
		{name: "apikey list by user", call: func() error {
			_, _, err := apiKeyStore.ListByUser(ctx, 1, appapikey.ListFilter{Page: 1, PageSize: 10})
			return err
		}},
		{name: "apikey list admin", call: func() error {
			_, _, err := apiKeyStore.ListAdmin(ctx, appapikey.ListFilter{Page: 1, PageSize: 10})
			return err
		}},
		{name: "apikey group access", call: func() error {
			_, err := apiKeyStore.GetGroupAccess(ctx, 1, 1)
			return err
		}},
		{name: "apikey delete owned", call: func() error {
			_, err := apiKeyStore.DeleteOwned(ctx, 1, 1)
			return err
		}},
		{name: "apikey load by id", call: func() error {
			_, err := apiKeyStore.loadByID(ctx, 1)
			return err
		}},
		{name: "apikey find owned", call: func() error {
			_, err := apiKeyStore.FindOwned(ctx, 1, 1)
			return err
		}},
		{name: "subscription list by user", call: func() error {
			_, _, err := subscriptionStore.ListByUser(ctx, appsubscription.UserListFilter{UserID: 1, Page: 1, PageSize: 10})
			return err
		}},
		{name: "subscription active", call: func() error {
			_, err := subscriptionStore.ListActiveByUser(ctx, 1)
			return err
		}},
		{name: "subscription list admin", call: func() error {
			_, _, err := subscriptionStore.ListAdmin(ctx, appsubscription.AdminListFilter{Page: 1, PageSize: 10})
			return err
		}},
		{name: "subscription create", call: func() error {
			_, err := subscriptionStore.Create(ctx, appsubscription.CreateInput{UserID: 1, GroupID: 1, EffectiveAt: now, ExpiresAt: now.Add(time.Hour), Status: "active"})
			return err
		}},
		{name: "subscription bulk create", call: func() error {
			_, err := subscriptionStore.BulkCreate(ctx, appsubscription.BulkCreateInput{UserIDs: []int{1}, GroupID: 1, EffectiveAt: now, ExpiresAt: now.Add(time.Hour), Status: "active"})
			return err
		}},
		{name: "subscription update", call: func() error {
			status := "active"
			_, err := subscriptionStore.Update(ctx, 1, appsubscription.UpdateInput{Status: &status})
			return err
		}},
		{name: "subscription find one", call: func() error {
			_, err := subscriptionStore.findOneWithEdges(ctx, 1)
			return err
		}},
		{name: "user find", call: func() error {
			_, err := userStore.FindByID(ctx, 1, true)
			return err
		}},
		{name: "user list", call: func() error {
			_, _, err := userStore.List(ctx, appuser.ListFilter{Page: 1, PageSize: 10})
			return err
		}},
		{name: "user email exists", call: func() error {
			_, err := userStore.EmailExists(ctx, "closed@example.test")
			return err
		}},
		{name: "user group rates", call: func() error {
			_, err := userStore.ListWithGroupRateOverride(ctx, 1)
			return err
		}},
		{name: "user update balance", call: func() error {
			_, err := userStore.UpdateBalance(ctx, 1, appuser.BalanceUpdate{Action: "add", Amount: 1})
			return err
		}},
		{name: "user delete", call: func() error {
			return userStore.Delete(ctx, 1)
		}},
		{name: "user balance logs", call: func() error {
			_, _, err := userStore.ListBalanceLogs(ctx, 1, 1, 10)
			return err
		}},
		{name: "user api key name", call: func() error {
			_, err := userStore.GetAPIKeyName(ctx, 1)
			return err
		}},
		{name: "user api key info", call: func() error {
			_, err := userStore.GetAPIKeyInfo(ctx, 1)
			return err
		}},
		{name: "user list api keys", call: func() error {
			_, _, err := userStore.ListAPIKeys(ctx, 1, 1, 10, now)
			return err
		}},
		{name: "user balance alert", call: func() error {
			return userStore.UpdateBalanceAlert(ctx, 1, 1)
		}},
		{name: "user balance alert notified", call: func() error {
			return userStore.SetBalanceAlertNotified(ctx, 1, true)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected closed database error")
			}
		})
	}
}
