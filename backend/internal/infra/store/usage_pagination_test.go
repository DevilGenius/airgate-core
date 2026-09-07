package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
)

func TestUsagePageIndexMatchesFiltersAndImmutablePages(t *testing.T) {
	db := enttestOpen(t)
	defer db.Close()
	ctx := t.Context()
	u1 := createTestUser(t, db, "pages-one@example.test")
	u2 := createTestUser(t, db, "pages-two@example.test")
	key, err := db.APIKey.Create().SetName("pages-key").SetKeyHash("pages-key-hash").SetUser(u1).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	group, err := db.Group.Create().SetName("pages-group").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.Account.Create().SetName("pages-account").SetEmail("pages-account@example.test").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 9, 6, 20, 0, 0, 0, time.UTC)
	var created []*ent.UsageLog
	for i := 0; i < 150; i++ {
		user := u1
		if i%3 == 0 {
			user = u2
		}
		model := "gpt-5.5"
		if i%4 == 0 {
			model = "gpt-image-2"
		}
		platform := "openai"
		if i%9 == 0 {
			platform = "claude"
		}
		build := db.UsageLog.Create().SetBillingEventID(fmt.Sprintf("page-test-%d", i)).SetPlatform(platform).SetModel(model).SetUser(user).SetCreatedAt(date.Add(time.Duration(i) * time.Hour))
		if i%5 != 0 {
			build.SetUserIDSnapshot(user.ID)
		} // Legacy owner relation remains queryable.
		if user.ID == u1.ID && i%2 == 0 {
			build.SetAPIKey(key)
		}
		if i%2 == 0 {
			build.SetAccount(account).SetGroup(group)
		}
		row, err := build.Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, row)
	}
	// Real ID gaps, not arithmetic assumptions about MAX(id).
	if err := db.UsageLog.DeleteOne(created[45]).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	store := NewUsageStore(db)
	uid, kid, aid, gid := int64(u1.ID), int64(key.ID), int64(account.ID), int64(group.ID)
	filters := []appusage.ListFilter{{}, {UserID: &uid}, {UserID: &uid, APIKeyID: &kid, ScopedToKey: true}, {AccountID: &aid, GroupID: &gid}, {AccountSearch: "ACCOUNT@EXAMPLE", Platform: "openai"}, {Model: "gpt,!image"}, {StartDate: "2026-09-07", EndDate: "2026-09-08", TZ: "Asia/Shanghai"}, {Model: "no-such-model"}}
	for n, filter := range filters {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			filter.Page, filter.PageSize = 1, 500
			logs, _, _, err := store.ListAdmin(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			index, err := store.BuildPageIndex(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			if index.Total != int64(len(logs)) {
				t.Fatalf("count=%d want=%d", index.Total, len(logs))
			}
			for page := 1; page <= max(1, (len(logs)+19)/20); page++ {
				ids, actual := index.Page(page, 20)
				want := []int64{}
				for _, item := range logs[min((page-1)*20, len(logs)):min(page*20, len(logs))] {
					want = append(want, item.ID)
				}
				if actual != page || !reflect.DeepEqual(ids, want) {
					t.Fatalf("page=%d got=%v want=%v", page, ids, want)
				}
			}
		})
	}
	service := appusage.NewService(store)
	filter := appusage.ListFilter{Page: 1, PageSize: 20, TZ: "UTC"}
	var metadata appusage.PaginationInfo
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		metadata, err = service.Pagination(ctx, uid, filter, false)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Status == "ready" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if metadata.Status != "ready" {
		t.Fatalf("metadata=%+v", metadata)
	}
	filter.Snapshot = metadata.Snapshot
	filter.Page = 3
	before, err := service.ListUser(ctx, uid, filter)
	if err != nil {
		t.Fatal(err)
	}
	if !before.TotalExact || before.Total != metadata.Total || before.Page != 3 {
		t.Fatalf("page=%+v", before)
	}
	newLog, err := db.UsageLog.Create().SetBillingEventID("new-after-page-snapshot").SetPlatform("openai").SetModel("gpt-5.5").SetUser(u1).SetUserIDSnapshot(u1.ID).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := service.ListUser(ctx, uid, filter)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("new insert shifted pinned page")
	}
	live, err := service.ListUser(ctx, uid, appusage.ListFilter{PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if live.List[0].ID != int64(newLog.ID) {
		t.Fatal("live first page missed newest record")
	}
	if _, err := service.ListUser(ctx, int64(u2.ID), filter); !errors.Is(err, appusage.ErrPageIndexExpired) {
		t.Fatalf("cross-user snapshot error=%v", err)
	}
	if err := db.UsageLog.DeleteOneID(int(before.List[0].ID)).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListUser(context.Background(), uid, filter); !errors.Is(err, appusage.ErrPageIndexExpired) {
		t.Fatalf("deleted snapshot record error=%v", err)
	}
}
