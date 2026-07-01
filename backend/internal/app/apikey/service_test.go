package apikey

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	corauth "github.com/DevilGenius/airgate-core/internal/auth"
)

const testAPIKeySecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func TestDisplayKeyPrefixPrefersHint(t *testing.T) {
	prefix := DisplayKeyPrefix(Key{
		KeyHint: "sk-abcd...wxyz",
		KeyHash: "1234567890abcdef",
	})
	if prefix != "sk-abcd...wxyz" {
		t.Fatalf("expected hint to be used, got %q", prefix)
	}
}

func TestDisplayKeyPrefixMasksPlainKey(t *testing.T) {
	prefix := DisplayKeyPrefix(Key{PlainKey: "sk-aaaabbbbccccdddd"})
	if prefix != "sk-aaaa...dddd" {
		t.Fatalf("expected masked plain key, got %q", prefix)
	}
}

func TestParseExpiresAtRejectsInvalidFormat(t *testing.T) {
	value := "2026/04/02"
	_, _, err := parseExpiresAt(&value)
	if err != ErrInvalidExpiresAt {
		t.Fatalf("expected ErrInvalidExpiresAt, got %v", err)
	}
}

func TestParseExpiresAtClearsWhenEmpty(t *testing.T) {
	value := ""
	expiresAt, hasExpiresAt, err := parseExpiresAt(&value)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !hasExpiresAt {
		t.Fatal("expected expires_at to be marked for update")
	}
	if expiresAt != nil {
		t.Fatalf("expected nil expires_at, got %v", expiresAt)
	}
}

func TestListByUserNormalizesPaginationAndAttachesUsage(t *testing.T) {
	var capturedFilter ListFilter
	var capturedIDs []int
	service := NewService(apiKeyStubRepository{
		listByUser: func(_ context.Context, userID int, filter ListFilter) ([]Key, int64, error) {
			if userID != 7 {
				t.Fatalf("用户 ID = %d，期望 7", userID)
			}
			capturedFilter = filter
			return []Key{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}, 2, nil
		},
		keyUsage: func(_ context.Context, keyIDs []int, todayStart time.Time) (map[int]UsageCosts, error) {
			capturedIDs = append([]int(nil), keyIDs...)
			if todayStart.IsZero() {
				t.Fatal("今日起点不应为空")
			}
			return map[int]UsageCosts{
				1: {TodaySalesCost: 1.2, TodayActualCost: 0.8},
				2: {ThirtyDaySalesCost: 9.8, ThirtyDayActualCost: 6.4},
			}, nil
		},
	}, testAPIKeySecret)

	result, err := service.ListByUser(t.Context(), 7, ListFilter{}, "Asia/Shanghai")
	if err != nil {
		t.Fatalf("查询 API Key 失败: %v", err)
	}
	if capturedFilter.Page != 1 || capturedFilter.PageSize != 20 {
		t.Fatalf("分页未归一化: %+v", capturedFilter)
	}
	if !reflect.DeepEqual(capturedIDs, []int{1, 2}) {
		t.Fatalf("用量查询 keyIDs = %v，期望 [1 2]", capturedIDs)
	}
	if result.Total != 2 || result.List[0].TodayCost != 1.2 || result.List[0].TodayActualCost != 0.8 ||
		result.List[1].ThirtyDayCost != 9.8 || result.List[1].ThirtyDayActualCost != 6.4 {
		t.Fatalf("列表结果异常: %+v", result)
	}
}

func TestListAdminNormalizesPaginationWithoutUsageAggregation(t *testing.T) {
	var capturedFilter ListFilter
	var usageCalled bool
	service := NewService(apiKeyStubRepository{
		listAdmin: func(_ context.Context, filter ListFilter) ([]Key, int64, error) {
			capturedFilter = filter
			return []Key{{ID: 3, Name: "admin-key"}}, 1, nil
		},
		keyUsage: func(_ context.Context, _ []int, _ time.Time) (map[int]UsageCosts, error) {
			usageCalled = true
			return nil, nil
		},
	}, testAPIKeySecret)

	result, err := service.ListAdmin(t.Context(), ListFilter{Keyword: "prod"})
	if err != nil {
		t.Fatalf("管理员查询 API Key 失败: %v", err)
	}
	if capturedFilter.Page != 1 || capturedFilter.PageSize != 20 || capturedFilter.Keyword != "prod" {
		t.Fatalf("分页或关键词未归一化: %+v", capturedFilter)
	}
	if usageCalled {
		t.Fatal("管理员搜索列表不应触发用量聚合")
	}
	if result.Total != 1 || len(result.List) != 1 || result.List[0].ID != 3 {
		t.Fatalf("列表结果异常: %+v", result)
	}
}

func TestListAdminAttachesUsageWhenRequested(t *testing.T) {
	var capturedIDs []int
	service := NewService(apiKeyStubRepository{
		listAdmin: func(_ context.Context, filter ListFilter) ([]Key, int64, error) {
			if !filter.IncludeUsage {
				t.Fatal("IncludeUsage 应保持为 true")
			}
			return []Key{{ID: 3, Name: "admin-key"}}, 1, nil
		},
		keyUsage: func(_ context.Context, keyIDs []int, todayStart time.Time) (map[int]UsageCosts, error) {
			capturedIDs = append([]int(nil), keyIDs...)
			if todayStart.IsZero() {
				t.Fatal("今日起点不应为空")
			}
			return map[int]UsageCosts{
				3: {
					TodaySalesCost:      2.5,
					TodayActualCost:     1,
					ThirtyDaySalesCost:  6.5,
					ThirtyDayActualCost: 3,
				},
			}, nil
		},
	}, testAPIKeySecret)

	result, err := service.ListAdmin(t.Context(), ListFilter{IncludeUsage: true, TZ: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("管理员查询 API Key 失败: %v", err)
	}
	if !reflect.DeepEqual(capturedIDs, []int{3}) {
		t.Fatalf("用量查询 keyIDs = %v，期望 [3]", capturedIDs)
	}
	if result.Total != 1 || result.List[0].TodayCost != 2.5 || result.List[0].TodayActualCost != 1 ||
		result.List[0].ThirtyDayCost != 6.5 || result.List[0].ThirtyDayActualCost != 3 {
		t.Fatalf("列表结果异常: %+v", result)
	}
}

func TestCreateOwnedBuildsMutationAndReturnsPlainKey(t *testing.T) {
	expiresAt := "2026-05-15T10:00:00Z"
	var captured Mutation
	service := NewService(apiKeyStubRepository{
		groupAccess: func(_ context.Context, userID, groupID int) (GroupAccess, error) {
			if userID != 7 || groupID != 3 {
				t.Fatalf("分组访问参数异常: user=%d group=%d", userID, groupID)
			}
			return GroupAccess{Exists: true, Allowed: true}, nil
		},
		create: func(_ context.Context, mutation Mutation) (Key, error) {
			captured = mutation
			return Key{ID: 10, Name: derefString(mutation.Name), UserID: derefInt(mutation.UserID)}, nil
		},
	}, testAPIKeySecret)

	item, err := service.CreateOwned(t.Context(), 7, CreateInput{
		Name:           "生产 Key",
		GroupID:        3,
		IPWhitelist:    []string{"127.0.0.1"},
		QuotaUSD:       99,
		SellRate:       float64Ptr(1.2),
		MaxConcurrency: -1,
		ExpiresAt:      &expiresAt,
	})
	if err != nil {
		t.Fatalf("创建 API Key 失败: %v", err)
	}
	if item.ID != 10 || item.PlainKey == "" {
		t.Fatalf("创建结果异常: %+v", item)
	}
	if derefString(captured.Name) != "生产 Key" || derefInt(captured.UserID) != 7 || derefInt(captured.GroupID) != 3 {
		t.Fatalf("基础 mutation 异常: %+v", captured)
	}
	if captured.SellRate == nil || *captured.SellRate != 1.2 {
		t.Fatalf("销售倍率 mutation = %+v，期望 1.2", captured.SellRate)
	}
	if !captured.HasIPWhitelist || len(captured.IPWhitelist) != 1 {
		t.Fatalf("IP 白名单 mutation 异常: %+v", captured)
	}
	if derefInt(captured.MaxConcurrency) != 0 {
		t.Fatalf("负并发上限应归零，得到 %+v", captured.MaxConcurrency)
	}
	if captured.KeyHint == nil || captured.KeyHash == nil || captured.KeyEncrypted == nil || captured.ExpiresAt == nil || !captured.HasExpiresAt {
		t.Fatalf("密钥或过期时间 mutation 缺失: %+v", captured)
	}
	plain, err := corauth.DecryptAPIKey(*captured.KeyEncrypted, testAPIKeySecret)
	if err != nil {
		t.Fatalf("创建密文无法解密: %v", err)
	}
	if plain != item.PlainKey {
		t.Fatalf("密文明文 = %q，期望返回的 PlainKey", plain)
	}
}

func TestCreateOwnedDefaultsSellRateToOne(t *testing.T) {
	var captured Mutation
	service := NewService(apiKeyStubRepository{
		create: func(_ context.Context, mutation Mutation) (Key, error) {
			captured = mutation
			return Key{ID: 10, Name: derefString(mutation.Name), UserID: derefInt(mutation.UserID)}, nil
		},
	}, testAPIKeySecret)

	_, err := service.CreateOwned(t.Context(), 7, CreateInput{Name: "默认倍率", GroupID: 3})
	if err != nil {
		t.Fatalf("创建 API Key 失败: %v", err)
	}
	if captured.SellRate == nil || *captured.SellRate != 1 {
		t.Fatalf("默认销售倍率 = %+v，期望 1", captured.SellRate)
	}
}

func TestCreateOwnedPreservesExplicitZeroSellRate(t *testing.T) {
	var captured Mutation
	service := NewService(apiKeyStubRepository{
		groupAccess: func(context.Context, int, int) (GroupAccess, error) {
			return GroupAccess{Exists: true, Allowed: true}, nil
		},
		create: func(_ context.Context, mutation Mutation) (Key, error) {
			captured = mutation
			return Key{ID: 10, Name: derefString(mutation.Name), UserID: derefInt(mutation.UserID)}, nil
		},
	}, testAPIKeySecret)

	zero := 0.0
	_, err := service.CreateOwned(t.Context(), 7, CreateInput{Name: "免费销售", GroupID: 3, SellRate: &zero})
	if err != nil {
		t.Fatalf("创建 API Key 失败: %v", err)
	}
	if captured.SellRate == nil || *captured.SellRate != 0 {
		t.Fatalf("显式 0 销售倍率 = %+v，期望 0", captured.SellRate)
	}
}

func TestCreateOwnedRejectsInvalidSellRate(t *testing.T) {
	service := NewService(apiKeyStubRepository{
		groupAccess: func(context.Context, int, int) (GroupAccess, error) {
			return GroupAccess{Exists: true, Allowed: true}, nil
		},
		create: func(context.Context, Mutation) (Key, error) {
			t.Fatal("invalid sell_rate must not be persisted")
			return Key{}, nil
		},
	}, testAPIKeySecret)

	for _, rate := range []float64{math.NaN(), -1, 0.001, 100.01, math.MaxFloat64} {
		if _, err := service.CreateOwned(t.Context(), 7, CreateInput{Name: "bad", GroupID: 3, SellRate: &rate}); !errors.Is(err, ErrInvalidSellRate) {
			t.Fatalf("sell_rate %v error = %v, want ErrInvalidSellRate", rate, err)
		}
	}
}

func TestCreateOwnedRejectsUnavailableGroup(t *testing.T) {
	service := NewService(apiKeyStubRepository{
		groupAccess: func(context.Context, int, int) (GroupAccess, error) {
			return GroupAccess{Exists: true, Allowed: false}, nil
		},
	}, testAPIKeySecret)

	_, err := service.CreateOwned(t.Context(), 7, CreateInput{GroupID: 3})
	if !errors.Is(err, ErrGroupForbidden) {
		t.Fatalf("错误 = %v，期望 ErrGroupForbidden", err)
	}
}

func TestCreateOwnedReturnsGenerateKeyError(t *testing.T) {
	previous := generateAPIKey
	t.Cleanup(func() { generateAPIKey = previous })
	generateErr := errors.New("generate failed")
	generateAPIKey = func() (string, string, error) {
		return "", "", generateErr
	}

	_, err := NewService(apiKeyStubRepository{}, testAPIKeySecret).CreateOwned(t.Context(), 7, CreateInput{GroupID: 3})
	if !errors.Is(err, generateErr) {
		t.Fatalf("CreateOwned generate error = %v, want %v", err, generateErr)
	}
}

func TestRevealOwnedDecryptsKeyAndRejectsLegacyKey(t *testing.T) {
	encrypted, err := corauth.EncryptAPIKey("sk-secret", testAPIKeySecret)
	if err != nil {
		t.Fatalf("准备密文失败: %v", err)
	}
	service := NewService(apiKeyStubRepository{
		findOwned: func(_ context.Context, _, id int) (Key, error) {
			if id == 1 {
				return Key{ID: id, KeyEncrypted: encrypted}, nil
			}
			return Key{ID: id}, nil
		},
	}, testAPIKeySecret)

	item, err := service.RevealOwned(t.Context(), 7, 1)
	if err != nil {
		t.Fatalf("查看明文失败: %v", err)
	}
	if item.PlainKey != "sk-secret" {
		t.Fatalf("明文 = %q，期望 sk-secret", item.PlainKey)
	}
	if _, err := service.RevealOwned(t.Context(), 7, 2); !errors.Is(err, ErrLegacyKeyNotReveal) {
		t.Fatalf("遗留 key 错误 = %v，期望 ErrLegacyKeyNotReveal", err)
	}
}

func TestUpdateOwnedBuildsMutationAndChecksGroup(t *testing.T) {
	name := "更新后的 Key"
	groupID := int64(8)
	sellRate := 0.0
	quota := 12.5
	clearExpiresAt := ""
	status := "disabled"
	var captured Mutation
	service := NewService(apiKeyStubRepository{
		groupAccess: func(_ context.Context, userID, groupID int) (GroupAccess, error) {
			if userID != 7 || groupID != 8 {
				t.Fatalf("分组访问参数异常: user=%d group=%d", userID, groupID)
			}
			return GroupAccess{Exists: true, Allowed: true}, nil
		},
		updateOwned: func(_ context.Context, userID, id int, mutation Mutation) (Key, error) {
			if userID != 7 || id != 11 {
				t.Fatalf("更新参数异常: user=%d id=%d", userID, id)
			}
			captured = mutation
			return Key{ID: id, Name: derefString(mutation.Name)}, nil
		},
	}, testAPIKeySecret)

	item, err := service.UpdateOwned(t.Context(), 7, 11, UpdateInput{
		Name:           &name,
		GroupID:        &groupID,
		IPBlacklist:    []string{"10.0.0.1"},
		HasIPBlacklist: true,
		QuotaUSD:       &quota,
		SellRate:       &sellRate,
		ExpiresAt:      &clearExpiresAt,
		Status:         &status,
	})
	if err != nil {
		t.Fatalf("更新 API Key 失败: %v", err)
	}
	if item.ID != 11 || item.Name != name {
		t.Fatalf("更新结果异常: %+v", item)
	}
	if derefString(captured.Name) != name || derefInt(captured.GroupID) != 8 || derefString(captured.Status) != "disabled" {
		t.Fatalf("mutation 字段异常: %+v", captured)
	}
	if !captured.HasIPBlacklist || len(captured.IPBlacklist) != 1 || !captured.HasExpiresAt || captured.ExpiresAt != nil {
		t.Fatalf("列表或过期时间 mutation 异常: %+v", captured)
	}
	if captured.SellRate == nil || *captured.SellRate != 0 {
		t.Fatalf("0 销售倍率应保留为免费，得到 %+v", captured.SellRate)
	}
	if captured.QuotaUSD == nil || *captured.QuotaUSD != quota {
		t.Fatalf("quota mutation = %+v，期望 %v", captured.QuotaUSD, quota)
	}
}

func TestUpdateAdminDoesNotCheckGroupAccess(t *testing.T) {
	groupID := int64(9)
	var checkedGroup bool
	service := NewService(apiKeyStubRepository{
		groupAccess: func(context.Context, int, int) (GroupAccess, error) {
			checkedGroup = true
			return GroupAccess{}, nil
		},
		updateAdmin: func(_ context.Context, id int, mutation Mutation) (Key, error) {
			return Key{ID: id, UserID: 42, GroupID: mutation.GroupID}, nil
		},
	}, testAPIKeySecret)

	item, err := service.UpdateAdmin(t.Context(), 13, UpdateInput{GroupID: &groupID})
	if err != nil {
		t.Fatalf("管理员更新失败: %v", err)
	}
	if checkedGroup {
		t.Fatal("管理员更新不应检查用户分组权限")
	}
	if item.GroupID == nil || *item.GroupID != 9 {
		t.Fatalf("更新分组异常: %+v", item.GroupID)
	}
}

func TestUpdateAdminBuildMutationErrors(t *testing.T) {
	badExpires := "2026-01-01"
	if _, err := NewService(apiKeyStubRepository{}, testAPIKeySecret).UpdateAdmin(t.Context(), 1, UpdateInput{ExpiresAt: &badExpires}); !errors.Is(err, ErrInvalidExpiresAt) {
		t.Fatalf("UpdateAdmin invalid expires error = %v", err)
	}

	badSellRate := -1.0
	if _, err := NewService(apiKeyStubRepository{}, testAPIKeySecret).UpdateAdmin(t.Context(), 1, UpdateInput{SellRate: &badSellRate}); !errors.Is(err, ErrInvalidSellRate) {
		t.Fatalf("UpdateAdmin invalid sell rate error = %v", err)
	}
}

func TestResetUsageAdminResetsRepositoryUsage(t *testing.T) {
	var capturedID int
	service := NewService(apiKeyStubRepository{
		resetUsageAdmin: func(_ context.Context, id int) (Key, error) {
			capturedID = id
			return Key{ID: id, UserID: 42, KeyHash: "hash-reset"}, nil
		},
	}, testAPIKeySecret)

	item, err := service.ResetUsageAdmin(t.Context(), 15)
	if err != nil {
		t.Fatalf("管理员重置 API Key 用量失败: %v", err)
	}
	if capturedID != 15 {
		t.Fatalf("重置 ID = %d，期望 15", capturedID)
	}
	if item.ID != 15 || item.UserID != 42 {
		t.Fatalf("重置结果异常: %+v", item)
	}
}

func TestListAndUsageErrorsPropagate(t *testing.T) {
	repoErr := errors.New("repo failed")
	service := NewService(apiKeyStubRepository{
		listByUser: func(context.Context, int, ListFilter) ([]Key, int64, error) { return nil, 0, repoErr },
		listAdmin:  func(context.Context, ListFilter) ([]Key, int64, error) { return nil, 0, repoErr },
		keyUsage:   func(context.Context, []int, time.Time) (map[int]UsageCosts, error) { return nil, repoErr },
	}, testAPIKeySecret)

	if _, err := service.ListByUser(t.Context(), 7, ListFilter{}, "UTC"); !errors.Is(err, repoErr) {
		t.Fatalf("ListByUser list error = %v", err)
	}
	if _, err := service.ListAdmin(t.Context(), ListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("ListAdmin list error = %v", err)
	}

	usageService := NewService(apiKeyStubRepository{
		listByUser: func(context.Context, int, ListFilter) ([]Key, int64, error) {
			return []Key{{ID: 1}}, 1, nil
		},
		listAdmin: func(context.Context, ListFilter) ([]Key, int64, error) {
			return []Key{{ID: 1}}, 1, nil
		},
		keyUsage: func(context.Context, []int, time.Time) (map[int]UsageCosts, error) {
			return nil, repoErr
		},
	}, testAPIKeySecret)
	if _, err := usageService.ListByUser(t.Context(), 7, ListFilter{}, "UTC"); !errors.Is(err, repoErr) {
		t.Fatalf("ListByUser usage error = %v", err)
	}
	if _, err := usageService.ListAdmin(t.Context(), ListFilter{IncludeUsage: true}); !errors.Is(err, repoErr) {
		t.Fatalf("ListAdmin usage error = %v", err)
	}
}

func TestCreateOwnedErrorBranches(t *testing.T) {
	repoErr := errors.New("repo failed")
	if _, err := NewService(apiKeyStubRepository{
		groupAccess: func(context.Context, int, int) (GroupAccess, error) { return GroupAccess{}, repoErr },
	}, testAPIKeySecret).CreateOwned(t.Context(), 7, CreateInput{GroupID: 3}); !errors.Is(err, repoErr) {
		t.Fatalf("CreateOwned group access error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		groupAccess: func(context.Context, int, int) (GroupAccess, error) {
			return GroupAccess{Exists: false}, nil
		},
	}, testAPIKeySecret).CreateOwned(t.Context(), 7, CreateInput{GroupID: 3}); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("CreateOwned missing group error = %v", err)
	}
	badExpires := "2026-01-01"
	if _, err := NewService(apiKeyStubRepository{}, testAPIKeySecret).CreateOwned(t.Context(), 7, CreateInput{GroupID: 3, ExpiresAt: &badExpires}); !errors.Is(err, ErrInvalidExpiresAt) {
		t.Fatalf("CreateOwned invalid expires error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		create: func(context.Context, Mutation) (Key, error) { return Key{}, repoErr },
	}, testAPIKeySecret).CreateOwned(t.Context(), 7, CreateInput{GroupID: 3}); !errors.Is(err, repoErr) {
		t.Fatalf("CreateOwned persist error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{}, "bad-secret").CreateOwned(t.Context(), 7, CreateInput{GroupID: 3}); err == nil {
		t.Fatal("CreateOwned bad secret error = nil, want encryption error")
	}
}

func TestUpdateAndResetErrorBranches(t *testing.T) {
	repoErr := errors.New("repo failed")
	badExpires := "2026-01-01"
	badSellRate := -1.0
	groupID := int64(3)

	if _, err := NewService(apiKeyStubRepository{}, testAPIKeySecret).UpdateOwned(t.Context(), 7, 1, UpdateInput{ExpiresAt: &badExpires}); !errors.Is(err, ErrInvalidExpiresAt) {
		t.Fatalf("UpdateOwned invalid expires error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{}, testAPIKeySecret).UpdateOwned(t.Context(), 7, 1, UpdateInput{SellRate: &badSellRate}); !errors.Is(err, ErrInvalidSellRate) {
		t.Fatalf("UpdateOwned invalid sell rate error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		groupAccess: func(context.Context, int, int) (GroupAccess, error) {
			return GroupAccess{Exists: true, Allowed: false}, nil
		},
	}, testAPIKeySecret).UpdateOwned(t.Context(), 7, 1, UpdateInput{GroupID: &groupID}); !errors.Is(err, ErrGroupForbidden) {
		t.Fatalf("UpdateOwned group forbidden error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		updateOwned: func(context.Context, int, int, Mutation) (Key, error) { return Key{}, repoErr },
	}, testAPIKeySecret).UpdateOwned(t.Context(), 7, 1, UpdateInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("UpdateOwned persist error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		updateAdmin: func(context.Context, int, Mutation) (Key, error) { return Key{}, repoErr },
	}, testAPIKeySecret).UpdateAdmin(t.Context(), 1, UpdateInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("UpdateAdmin persist error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		resetUsageAdmin: func(context.Context, int) (Key, error) { return Key{}, repoErr },
	}, testAPIKeySecret).ResetUsageAdmin(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("ResetUsageAdmin error = %v", err)
	}
}

func TestDeleteOwnedAndRevealErrors(t *testing.T) {
	repoErr := errors.New("repo failed")
	var deleted bool
	service := NewService(apiKeyStubRepository{
		deleteOwned: func(_ context.Context, userID, id int) (Key, error) {
			if userID != 7 || id != 1 {
				t.Fatalf("DeleteOwned args = %d/%d", userID, id)
			}
			deleted = true
			return Key{KeyHash: "deleted-hash"}, nil
		},
	}, testAPIKeySecret)
	if err := service.DeleteOwned(t.Context(), 7, 1); err != nil || !deleted {
		t.Fatalf("DeleteOwned() = %v deleted=%v", err, deleted)
	}
	if err := NewService(apiKeyStubRepository{
		deleteOwned: func(context.Context, int, int) (Key, error) { return Key{}, repoErr },
	}, testAPIKeySecret).DeleteOwned(t.Context(), 7, 1); !errors.Is(err, repoErr) {
		t.Fatalf("DeleteOwned error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		findOwned: func(context.Context, int, int) (Key, error) { return Key{}, repoErr },
	}, testAPIKeySecret).RevealOwned(t.Context(), 7, 1); !errors.Is(err, repoErr) {
		t.Fatalf("RevealOwned find error = %v", err)
	}
	if _, err := NewService(apiKeyStubRepository{
		findOwned: func(context.Context, int, int) (Key, error) { return Key{KeyEncrypted: "bad-ciphertext"}, nil },
	}, testAPIKeySecret).RevealOwned(t.Context(), 7, 1); !errors.Is(err, ErrKeyDecryptFailed) {
		t.Fatalf("RevealOwned decrypt error = %v", err)
	}
}

func TestDisplayKeyPrefixFallbacks(t *testing.T) {
	if got := DisplayKeyPrefix(Key{KeyHash: "abcdef1234567890"}); got != "sk-abcdef12..." {
		t.Fatalf("hash display prefix = %q", got)
	}
	if got := DisplayKeyPrefix(Key{KeyHash: "short"}); got != "short" {
		t.Fatalf("short hash display prefix = %q", got)
	}
	if got := buildKeyHint("short-key"); got != "short-key" {
		t.Fatalf("short buildKeyHint = %q", got)
	}
}

func TestNormalizeOptionalHelpers(t *testing.T) {
	if got := normalizeBalanceAlertThreshold(-1); got != 0 {
		t.Fatalf("negative threshold = %v, want 0", got)
	}
	threshold := -3.0
	if got := normalizeOptionalBalanceAlertThreshold(&threshold); got == nil || *got != 0 {
		t.Fatalf("optional threshold = %+v, want 0", got)
	}
	email := " alert@example.com "
	if got := normalizeOptionalString(&email); got == nil || *got != "alert@example.com" {
		t.Fatalf("optional string = %+v, want trimmed email", got)
	}
}

type apiKeyStubRepository struct {
	listByUser      func(context.Context, int, ListFilter) ([]Key, int64, error)
	listAdmin       func(context.Context, ListFilter) ([]Key, int64, error)
	keyUsage        func(context.Context, []int, time.Time) (map[int]UsageCosts, error)
	groupAccess     func(context.Context, int, int) (GroupAccess, error)
	create          func(context.Context, Mutation) (Key, error)
	updateOwned     func(context.Context, int, int, Mutation) (Key, error)
	updateAdmin     func(context.Context, int, Mutation) (Key, error)
	resetUsageAdmin func(context.Context, int) (Key, error)
	deleteOwned     func(context.Context, int, int) (Key, error)
	findOwned       func(context.Context, int, int) (Key, error)
}

func (s apiKeyStubRepository) ListByUser(ctx context.Context, userID int, filter ListFilter) ([]Key, int64, error) {
	if s.listByUser == nil {
		return nil, 0, nil
	}
	return s.listByUser(ctx, userID, filter)
}

func (s apiKeyStubRepository) ListAdmin(ctx context.Context, filter ListFilter) ([]Key, int64, error) {
	if s.listAdmin == nil {
		return nil, 0, nil
	}
	return s.listAdmin(ctx, filter)
}

func (s apiKeyStubRepository) KeyUsage(ctx context.Context, keyIDs []int, todayStart time.Time) (map[int]UsageCosts, error) {
	if s.keyUsage == nil {
		return map[int]UsageCosts{}, nil
	}
	return s.keyUsage(ctx, keyIDs, todayStart)
}

func (s apiKeyStubRepository) GetGroupAccess(ctx context.Context, userID, groupID int) (GroupAccess, error) {
	if s.groupAccess == nil {
		return GroupAccess{Exists: true, Allowed: true}, nil
	}
	return s.groupAccess(ctx, userID, groupID)
}

func (s apiKeyStubRepository) Create(ctx context.Context, mutation Mutation) (Key, error) {
	if s.create == nil {
		return Key{}, nil
	}
	return s.create(ctx, mutation)
}

func (s apiKeyStubRepository) UpdateOwned(ctx context.Context, userID, id int, mutation Mutation) (Key, error) {
	if s.updateOwned == nil {
		return Key{}, nil
	}
	return s.updateOwned(ctx, userID, id, mutation)
}

func (s apiKeyStubRepository) UpdateAdmin(ctx context.Context, id int, mutation Mutation) (Key, error) {
	if s.updateAdmin == nil {
		return Key{}, nil
	}
	return s.updateAdmin(ctx, id, mutation)
}

func (s apiKeyStubRepository) ResetUsageAdmin(ctx context.Context, id int) (Key, error) {
	if s.resetUsageAdmin == nil {
		return Key{}, nil
	}
	return s.resetUsageAdmin(ctx, id)
}

func (s apiKeyStubRepository) DeleteOwned(ctx context.Context, userID, id int) (Key, error) {
	if s.deleteOwned == nil {
		return Key{}, nil
	}
	return s.deleteOwned(ctx, userID, id)
}

func (s apiKeyStubRepository) FindOwned(ctx context.Context, userID, id int) (Key, error) {
	if s.findOwned == nil {
		return Key{}, nil
	}
	return s.findOwned(ctx, userID, id)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func float64Ptr(value float64) *float64 {
	return &value
}
