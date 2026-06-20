package user

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestGetAndUpdateProfile(t *testing.T) {
	var includeDetails bool
	service := NewService(stubRepository{
		findByIDFull: func(_ context.Context, id int, include bool) (User, error) {
			includeDetails = include
			return User{ID: id, Email: "user@example.com"}, nil
		},
		update: func(_ context.Context, id int, mutation Mutation) (User, error) {
			if id != 7 || mutation.Username == nil || *mutation.Username != "next" {
				t.Fatalf("unexpected update mutation: id=%d mutation=%+v", id, mutation)
			}
			return User{ID: id, Username: *mutation.Username}, nil
		},
	})

	got, err := service.Get(t.Context(), 7)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != 7 || !includeDetails {
		t.Fatalf("Get() = %+v include=%v", got, includeDetails)
	}
	updated, err := service.UpdateProfile(t.Context(), 7, "next")
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Username != "next" {
		t.Fatalf("UpdateProfile() = %+v", updated)
	}
}

func TestChangePassword(t *testing.T) {
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash old password: %v", err)
	}
	var persistedHash string
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, PasswordHash: string(oldHash)}, nil
		},
		update: func(_ context.Context, id int, mutation Mutation) (User, error) {
			if id != 1 || mutation.PasswordHash == nil {
				t.Fatalf("unexpected password mutation: id=%d mutation=%+v", id, mutation)
			}
			persistedHash = *mutation.PasswordHash
			return User{ID: id}, nil
		},
	})

	if err := service.ChangePassword(t.Context(), 1, "old-password", "new-password"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(persistedHash), []byte("new-password")); err != nil {
		t.Fatalf("persisted hash does not match new password: %v", err)
	}

	err = service.ChangePassword(t.Context(), 1, "wrong", "new-password")
	if err != ErrOldPasswordMismatch {
		t.Fatalf("ChangePassword wrong old error = %v, want ErrOldPasswordMismatch", err)
	}
}

func TestPasswordHashErrors(t *testing.T) {
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash old password: %v", err)
	}
	tooLong := strings.Repeat("x", 73)

	if err := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, PasswordHash: string(oldHash)}, nil
		},
	}).ChangePassword(t.Context(), 1, "old-password", tooLong); !errors.Is(err, bcrypt.ErrPasswordTooLong) {
		t.Fatalf("ChangePassword long password error = %v, want bcrypt.ErrPasswordTooLong", err)
	}

	if _, err := NewService(stubRepository{}).Create(t.Context(), CreateInput{
		Email:    "new@example.com",
		Password: tooLong,
	}); !errors.Is(err, bcrypt.ErrPasswordTooLong) {
		t.Fatalf("Create long password error = %v, want bcrypt.ErrPasswordTooLong", err)
	}

	if _, err := NewService(stubRepository{}).Update(t.Context(), 1, UpdateInput{
		Password: &tooLong,
	}); !errors.Is(err, bcrypt.ErrPasswordTooLong) {
		t.Fatalf("Update long password error = %v, want bcrypt.ErrPasswordTooLong", err)
	}
}

func TestListCreateAndUpdate(t *testing.T) {
	var listFilter ListFilter
	var createMutation Mutation
	var updateMutation Mutation
	password := "updated-password"
	role := "admin"
	status := "disabled"
	maxConcurrency := 4
	service := NewService(stubRepository{
		list: func(_ context.Context, filter ListFilter) ([]User, int64, error) {
			listFilter = filter
			return []User{{ID: 1}}, 3, nil
		},
		emailExists: func(_ context.Context, email string) (bool, error) {
			if email != "new@example.com" {
				t.Fatalf("EmailExists email = %q", email)
			}
			return false, nil
		},
		create: func(_ context.Context, mutation Mutation) (User, error) {
			createMutation = mutation
			return User{ID: 9, Email: *mutation.Email, Username: *mutation.Username, Role: *mutation.Role}, nil
		},
		update: func(_ context.Context, id int, mutation Mutation) (User, error) {
			if id != 9 {
				t.Fatalf("Update id = %d, want 9", id)
			}
			updateMutation = mutation
			return User{ID: id, Status: *mutation.Status}, nil
		},
	})

	list, err := service.List(t.Context(), ListFilter{Page: -1, PageSize: 0, Keyword: "alice"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.Page != 1 || list.PageSize != 20 || list.Total != 3 || listFilter.Page != 1 || listFilter.PageSize != 20 || listFilter.Keyword != "alice" {
		t.Fatalf("List() result=%+v filter=%+v", list, listFilter)
	}

	created, err := service.Create(t.Context(), CreateInput{
		Email:          "new@example.com",
		Password:       "secret",
		Username:       "new",
		Role:           "user",
		MaxConcurrency: 3,
		GroupRates:     map[int64]float64{8: 1.25},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID != 9 || createMutation.Email == nil || *createMutation.Email != "new@example.com" || createMutation.MaxConcurrency == nil || *createMutation.MaxConcurrency != 3 {
		t.Fatalf("Create() mutation=%+v created=%+v", createMutation, created)
	}
	if createMutation.GroupRates[8] != 1.25 || !createMutation.HasGroupRates {
		t.Fatalf("Create() group rates mutation=%+v", createMutation)
	}
	createMutation.GroupRates[8] = 9

	updated, err := service.Update(t.Context(), 9, UpdateInput{
		Password:           &password,
		Role:               &role,
		MaxConcurrency:     &maxConcurrency,
		GroupRates:         map[int64]float64{8: 0.75},
		HasGroupRates:      true,
		AllowedGroupIDs:    []int64{8, 9},
		HasAllowedGroupIDs: true,
		Status:             &status,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Status != "disabled" || updateMutation.PasswordHash == nil || updateMutation.Role == nil || *updateMutation.Role != "admin" || updateMutation.MaxConcurrency == nil || *updateMutation.MaxConcurrency != 4 {
		t.Fatalf("Update() mutation=%+v updated=%+v", updateMutation, updated)
	}
	if updateMutation.GroupRates[8] != 0.75 || updateMutation.AllowedGroupIDs[1] != 9 || !updateMutation.HasAllowedGroupIDs {
		t.Fatalf("Update() group mutation=%+v", updateMutation)
	}
}

func TestCreateRejectsExistingEmail(t *testing.T) {
	service := NewService(stubRepository{
		emailExists: func(context.Context, string) (bool, error) { return true, nil },
	})

	_, err := service.Create(t.Context(), CreateInput{Email: "exists@example.com", Password: "secret"})
	if err != ErrEmailAlreadyExists {
		t.Fatalf("Create() error = %v, want ErrEmailAlreadyExists", err)
	}
}

func TestAdjustBalanceRejectsInvalidAction(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Balance: 10}, nil
		},
	})

	_, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "noop", Amount: 1})
	if err != ErrInvalidBalanceAction {
		t.Fatalf("expected ErrInvalidBalanceAction, got %v", err)
	}
}

func TestAdjustBalanceRejectsInsufficientBalance(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Balance: 5}, nil
		},
	})

	_, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "subtract", Amount: 10})
	if err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestAdjustBalanceActionsAndAlert(t *testing.T) {
	alerts := make(chan string, 1)
	var update BalanceUpdate
	var notified []bool
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Email: "user@example.com", Balance: 20}, nil
		},
		updateBalance: func(_ context.Context, id int, input BalanceUpdate) (User, error) {
			if id != 1 {
				t.Fatalf("UpdateBalance id = %d, want 1", id)
			}
			update = input
			return User{
				ID:                    id,
				Email:                 "user@example.com",
				Balance:               input.AfterBalance,
				BalanceAlertThreshold: 10,
			}, nil
		},
		setBalanceAlertNotified: func(_ context.Context, _ int, value bool) error {
			notified = append(notified, value)
			return nil
		},
	})
	service.SetBalanceAlertCallback(func(email string, balance float64, threshold float64) {
		alerts <- email
	})

	updated, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "subtract", Amount: 15, Remark: "usage"})
	if err != nil {
		t.Fatalf("AdjustBalance() error = %v", err)
	}
	if updated.Balance != 5 || update.BeforeBalance != 20 || update.AfterBalance != 5 || update.Remark != "usage" {
		t.Fatalf("updated=%+v update=%+v", updated, update)
	}
	if len(notified) != 1 || !notified[0] {
		t.Fatalf("notified = %#v, want [true]", notified)
	}
	select {
	case got := <-alerts:
		if got != "user@example.com" {
			t.Fatalf("alert email = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for balance alert")
	}
}

func TestCheckBalanceAlertResetsNotification(t *testing.T) {
	var notified []bool
	service := NewService(stubRepository{
		setBalanceAlertNotified: func(_ context.Context, _ int, value bool) error {
			notified = append(notified, value)
			return nil
		},
	})
	service.SetBalanceAlertCallback(func(string, float64, float64) {})

	service.checkBalanceAlert(t.Context(), User{
		ID:                    1,
		Balance:               15,
		BalanceAlertThreshold: 10,
		BalanceAlertNotified:  true,
	}, 5)
	if len(notified) != 1 || notified[0] {
		t.Fatalf("notified = %#v, want [false]", notified)
	}
}

func TestUpdateBalanceAlertAndGroupRateDelegates(t *testing.T) {
	var threshold float64
	var overrideGroupID int64
	service := NewService(stubRepository{
		updateBalanceAlert: func(_ context.Context, userID int, value float64) error {
			if userID != 7 {
				t.Fatalf("UpdateBalanceAlert userID = %d", userID)
			}
			threshold = value
			return nil
		},
		listOverrides: func(_ context.Context, groupID int64) ([]GroupRateOverride, error) {
			overrideGroupID = groupID
			return []GroupRateOverride{{UserID: 7, Rate: 0.5}}, nil
		},
	})

	if err := service.UpdateBalanceAlert(t.Context(), 7, 3.5); err != nil {
		t.Fatalf("UpdateBalanceAlert() error = %v", err)
	}
	if threshold != 3.5 {
		t.Fatalf("threshold = %v, want 3.5", threshold)
	}
	overrides, err := service.ListGroupRateOverrides(t.Context(), 9)
	if err != nil {
		t.Fatalf("ListGroupRateOverrides() error = %v", err)
	}
	if overrideGroupID != 9 || len(overrides) != 1 || overrides[0].Rate != 0.5 {
		t.Fatalf("overrides=%+v group=%d", overrides, overrideGroupID)
	}
}

func TestListAPIKeysNormalizesPagination(t *testing.T) {
	service := NewService(stubRepository{
		listAPIKeys: func(_ context.Context, _ int, page, pageSize int) ([]APIKey, int64, error) {
			if page != 1 || pageSize != 20 {
				t.Fatalf("ListAPIKeys received page=%d pageSize=%d, want 1 and 20", page, pageSize)
			}
			return []APIKey{{ID: 1}}, 1, nil
		},
	})

	result, err := service.ListAPIKeys(t.Context(), 7, 0, 0, "")
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if result.Page != 1 || result.PageSize != 20 || result.Total != 1 || len(result.List) != 1 {
		t.Fatalf("unexpected ListAPIKeys result: %+v", result)
	}
}

func TestSetGroupRateAllowsMinimumPositiveRate(t *testing.T) {
	var captured Mutation
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Email: "user@example.com", GroupRates: map[int64]float64{9: 0.5}}, nil
		},
		update: func(_ context.Context, _ int, mutation Mutation) (User, error) {
			captured = mutation
			return User{ID: 1, Email: "user@example.com", GroupRates: mutation.GroupRates}, nil
		},
	})

	if _, err := service.SetGroupRate(t.Context(), 1, 9, 0.01); err != nil {
		t.Fatalf("SetGroupRate(rate=0.01) returned error: %v", err)
	}
	if captured.GroupRates[9] != 0.01 {
		t.Fatalf("captured rate = %v, want 0.01", captured.GroupRates[9])
	}
}

func TestSetGroupRateHandlesRepositoryBranches(t *testing.T) {
	repoErr := errors.New("repo failed")
	if _, err := NewService(stubRepository{
		findByID: func() (User, error) { return User{}, repoErr },
	}).SetGroupRate(t.Context(), 1, 9, 0.5); !errors.Is(err, repoErr) {
		t.Fatalf("SetGroupRate find error = %v", err)
	}

	var captured Mutation
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Email: "user@example.com"}, nil
		},
		update: func(_ context.Context, _ int, mutation Mutation) (User, error) {
			captured = mutation
			return User{ID: 1, Email: "user@example.com"}, nil
		},
	})
	override, err := service.SetGroupRate(t.Context(), 1, 9, 0.5)
	if err != nil {
		t.Fatalf("SetGroupRate nil map error = %v", err)
	}
	if override.Rate != 0.5 || captured.GroupRates[9] != 0.5 || !captured.HasGroupRates {
		t.Fatalf("SetGroupRate nil map override=%+v mutation=%+v", override, captured)
	}

	if _, err := NewService(stubRepository{
		findByID: func() (User, error) { return User{ID: 1, GroupRates: map[int64]float64{9: 0.4}}, nil },
		update:   func(context.Context, int, Mutation) (User, error) { return User{}, repoErr },
	}).SetGroupRate(t.Context(), 1, 9, 0.5); !errors.Is(err, repoErr) {
		t.Fatalf("SetGroupRate update error = %v", err)
	}
}

func TestDeleteGroupRate(t *testing.T) {
	var updates int
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Email: "user@example.com", GroupRates: map[int64]float64{7: 0.5, 8: 1.5}}, nil
		},
		update: func(_ context.Context, _ int, mutation Mutation) (User, error) {
			updates++
			if _, ok := mutation.GroupRates[7]; ok || mutation.GroupRates[8] != 1.5 || !mutation.HasGroupRates {
				t.Fatalf("DeleteGroupRate mutation = %+v", mutation)
			}
			return User{ID: 1, GroupRates: mutation.GroupRates}, nil
		},
	})

	if err := service.DeleteGroupRate(t.Context(), 1, 7); err != nil {
		t.Fatalf("DeleteGroupRate() error = %v", err)
	}
	if err := service.DeleteGroupRate(t.Context(), 1, 9); err != nil {
		t.Fatalf("DeleteGroupRate(missing) error = %v", err)
	}
	if updates != 1 {
		t.Fatalf("updates = %d, want 1", updates)
	}
}

func TestDeleteGroupRatePropagatesFindError(t *testing.T) {
	repoErr := errors.New("repo failed")
	if err := NewService(stubRepository{
		findByID: func() (User, error) { return User{}, repoErr },
	}).DeleteGroupRate(t.Context(), 1, 7); !errors.Is(err, repoErr) {
		t.Fatalf("DeleteGroupRate find error = %v", err)
	}
}

func TestSetGroupRateRejectsInvalidRate(t *testing.T) {
	service := NewService(stubRepository{})

	for _, rate := range []float64{0, 0.001} {
		_, err := service.SetGroupRate(t.Context(), 1, 9, rate)
		if !errors.Is(err, ErrInvalidRateMultiplier) {
			t.Fatalf("SetGroupRate(rate=%v) error = %v, want ErrInvalidRateMultiplier", rate, err)
		}
	}
}

func TestDeleteAndToggleStatus(t *testing.T) {
	status := "active"
	var deletedID int
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Role: "user", Status: status}, nil
		},
		delete: func(_ context.Context, id int) error {
			deletedID = id
			return nil
		},
		update: func(_ context.Context, id int, mutation Mutation) (User, error) {
			status = *mutation.Status
			return User{ID: id, Status: status}, nil
		},
	})

	if err := service.Delete(t.Context(), 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deletedID != 1 {
		t.Fatalf("deletedID = %d, want 1", deletedID)
	}
	got, err := service.ToggleStatus(t.Context(), 1)
	if err != nil {
		t.Fatalf("ToggleStatus() error = %v", err)
	}
	if got.Status != "disabled" {
		t.Fatalf("ToggleStatus active = %+v", got)
	}
	got, err = service.ToggleStatus(t.Context(), 1)
	if err != nil {
		t.Fatalf("ToggleStatus second error = %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("ToggleStatus disabled = %+v", got)
	}
}

func TestDeleteRejectsAdmin(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Role: "admin"}, nil
		},
	})

	if err := service.Delete(t.Context(), 1); err != ErrDeleteAdminForbidden {
		t.Fatalf("Delete(admin) error = %v, want ErrDeleteAdminForbidden", err)
	}
}

func TestUpdateRejectsInvalidGroupRate(t *testing.T) {
	service := NewService(stubRepository{})

	_, err := service.Update(t.Context(), 1, UpdateInput{
		GroupRates:    map[int64]float64{9: -1},
		HasGroupRates: true,
	})
	if !errors.Is(err, ErrInvalidRateMultiplier) {
		t.Fatalf("Update() error = %v, want ErrInvalidRateMultiplier", err)
	}
}

func TestBalanceLogsAndAPIKeyDelegates(t *testing.T) {
	expiresAt := time.Now()
	service := NewService(stubRepository{
		listBalanceLogs: func(_ context.Context, userID, page, pageSize int) ([]BalanceLog, int64, error) {
			if userID != 7 || page != 1 || pageSize != 20 {
				t.Fatalf("ListBalanceLogs args = %d/%d/%d", userID, page, pageSize)
			}
			return []BalanceLog{{ID: 1, Action: "add"}}, 2, nil
		},
		getAPIKeyName: func(_ context.Context, keyID int) (string, error) {
			if keyID != 9 {
				t.Fatalf("GetAPIKeyName keyID = %d", keyID)
			}
			return "primary", nil
		},
		getAPIKeyInfo: func(_ context.Context, keyID int) (APIKeyBrief, error) {
			if keyID != 9 {
				t.Fatalf("GetAPIKeyInfo keyID = %d", keyID)
			}
			return APIKeyBrief{Name: "primary", ExpiresAt: &expiresAt}, nil
		},
	})

	logs, err := service.ListBalanceLogs(t.Context(), 7, 0, 0)
	if err != nil {
		t.Fatalf("ListBalanceLogs() error = %v", err)
	}
	if logs.Total != 2 || logs.Page != 1 || logs.PageSize != 20 || len(logs.List) != 1 {
		t.Fatalf("logs = %+v", logs)
	}
	name, err := service.GetAPIKeyName(t.Context(), 9)
	if err != nil || name != "primary" {
		t.Fatalf("GetAPIKeyName() = %q/%v", name, err)
	}
	info, err := service.GetAPIKeyInfo(t.Context(), 9)
	if err != nil || info.Name != "primary" || info.ExpiresAt != &expiresAt {
		t.Fatalf("GetAPIKeyInfo() = %+v/%v", info, err)
	}
}

func TestServiceErrorBranches(t *testing.T) {
	repoErr := errors.New("repo failed")

	if _, err := NewService(stubRepository{
		update: func(context.Context, int, Mutation) (User, error) { return User{}, repoErr },
	}).UpdateProfile(t.Context(), 1, "name"); !errors.Is(err, repoErr) {
		t.Fatalf("UpdateProfile error = %v", err)
	}

	if err := NewService(stubRepository{
		findByID: func() (User, error) { return User{}, repoErr },
	}).ChangePassword(t.Context(), 1, "old", "new"); !errors.Is(err, repoErr) {
		t.Fatalf("ChangePassword lookup error = %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("old"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := NewService(stubRepository{
		findByID: func() (User, error) { return User{PasswordHash: string(hash)}, nil },
		update:   func(context.Context, int, Mutation) (User, error) { return User{}, repoErr },
	}).ChangePassword(t.Context(), 1, "old", "new"); !errors.Is(err, repoErr) {
		t.Fatalf("ChangePassword persist error = %v", err)
	}

	if _, err := NewService(stubRepository{
		list: func(context.Context, ListFilter) ([]User, int64, error) { return nil, 0, repoErr },
	}).List(t.Context(), ListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("List error = %v", err)
	}

	if _, err := NewService(stubRepository{}).Create(t.Context(), CreateInput{
		GroupRates: map[int64]float64{1: 0},
	}); !errors.Is(err, ErrInvalidRateMultiplier) {
		t.Fatalf("Create invalid group rate error = %v", err)
	}
	if _, err := NewService(stubRepository{
		emailExists: func(context.Context, string) (bool, error) { return false, repoErr },
	}).Create(t.Context(), CreateInput{Email: "new@example.com"}); !errors.Is(err, repoErr) {
		t.Fatalf("Create email check error = %v", err)
	}
	if _, err := NewService(stubRepository{
		create: func(context.Context, Mutation) (User, error) { return User{}, repoErr },
	}).Create(t.Context(), CreateInput{Email: "new@example.com", Password: "secret"}); !errors.Is(err, repoErr) {
		t.Fatalf("Create persist error = %v", err)
	}

	if _, err := NewService(stubRepository{
		update: func(context.Context, int, Mutation) (User, error) { return User{}, repoErr },
	}).Update(t.Context(), 1, UpdateInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("Update persist error = %v", err)
	}

	if _, err := NewService(stubRepository{
		findByID: func() (User, error) { return User{}, repoErr },
	}).AdjustBalance(t.Context(), 1, BalanceChange{Action: "add", Amount: 1}); !errors.Is(err, repoErr) {
		t.Fatalf("AdjustBalance lookup error = %v", err)
	}
	if _, err := NewService(stubRepository{
		findByID:      func() (User, error) { return User{Balance: 1}, nil },
		updateBalance: func(context.Context, int, BalanceUpdate) (User, error) { return User{}, repoErr },
	}).AdjustBalance(t.Context(), 1, BalanceChange{Action: "add", Amount: 1}); !errors.Is(err, repoErr) {
		t.Fatalf("AdjustBalance persist error = %v", err)
	}

	if err := NewService(stubRepository{
		findByID: func() (User, error) { return User{}, repoErr },
	}).Delete(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("Delete lookup error = %v", err)
	}
	if err := NewService(stubRepository{
		findByID: func() (User, error) { return User{Role: "user"}, nil },
		delete:   func(context.Context, int) error { return repoErr },
	}).Delete(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("Delete persist error = %v", err)
	}

	if _, err := NewService(stubRepository{
		findByID: func() (User, error) { return User{}, repoErr },
	}).ToggleStatus(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("ToggleStatus lookup error = %v", err)
	}
	if _, err := NewService(stubRepository{
		findByID: func() (User, error) { return User{Status: "active"}, nil },
		update:   func(context.Context, int, Mutation) (User, error) { return User{}, repoErr },
	}).ToggleStatus(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("ToggleStatus persist error = %v", err)
	}

	if _, err := NewService(stubRepository{
		listBalanceLogs: func(context.Context, int, int, int) ([]BalanceLog, int64, error) { return nil, 0, repoErr },
	}).ListBalanceLogs(t.Context(), 1, 1, 20); !errors.Is(err, repoErr) {
		t.Fatalf("ListBalanceLogs error = %v", err)
	}
	if _, err := NewService(stubRepository{
		listAPIKeys: func(context.Context, int, int, int) ([]APIKey, int64, error) { return nil, 0, repoErr },
	}).ListAPIKeys(t.Context(), 1, 1, 20, "UTC"); !errors.Is(err, repoErr) {
		t.Fatalf("ListAPIKeys error = %v", err)
	}
}

func TestAdjustBalanceSetAndAdd(t *testing.T) {
	updates := make([]BalanceUpdate, 0, 2)
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Balance: 10}, nil
		},
		updateBalance: func(_ context.Context, id int, input BalanceUpdate) (User, error) {
			updates = append(updates, input)
			return User{ID: id, Balance: input.AfterBalance}, nil
		},
	})

	if got, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "set", Amount: 3}); err != nil || got.Balance != 3 {
		t.Fatalf("set AdjustBalance() = %+v/%v", got, err)
	}
	if got, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "add", Amount: 2}); err != nil || got.Balance != 12 {
		t.Fatalf("add AdjustBalance() = %+v/%v", got, err)
	}
	if len(updates) != 2 || updates[0].AfterBalance != 3 || updates[1].AfterBalance != 12 {
		t.Fatalf("updates = %+v", updates)
	}
}

type stubRepository struct {
	findByID                func() (User, error)
	findByIDFull            func(context.Context, int, bool) (User, error)
	list                    func(context.Context, ListFilter) ([]User, int64, error)
	emailExists             func(context.Context, string) (bool, error)
	listOverrides           func(context.Context, int64) ([]GroupRateOverride, error)
	create                  func(context.Context, Mutation) (User, error)
	update                  func(context.Context, int, Mutation) (User, error)
	updateBalance           func(context.Context, int, BalanceUpdate) (User, error)
	delete                  func(context.Context, int) error
	listBalanceLogs         func(context.Context, int, int, int) ([]BalanceLog, int64, error)
	listAPIKeys             func(context.Context, int, int, int) ([]APIKey, int64, error)
	getAPIKeyName           func(context.Context, int) (string, error)
	getAPIKeyInfo           func(context.Context, int) (APIKeyBrief, error)
	updateBalanceAlert      func(context.Context, int, float64) error
	setBalanceAlertNotified func(context.Context, int, bool) error
}

func (s stubRepository) FindByID(ctx context.Context, id int, includeDetails bool) (User, error) {
	if s.findByIDFull != nil {
		return s.findByIDFull(ctx, id, includeDetails)
	}
	if s.findByID != nil {
		return s.findByID()
	}
	return User{}, nil
}

func (s stubRepository) List(ctx context.Context, filter ListFilter) ([]User, int64, error) {
	if s.list != nil {
		return s.list(ctx, filter)
	}
	return nil, 0, nil
}
func (s stubRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	if s.emailExists != nil {
		return s.emailExists(ctx, email)
	}
	return false, nil
}
func (s stubRepository) ListWithGroupRateOverride(ctx context.Context, groupID int64) ([]GroupRateOverride, error) {
	if s.listOverrides != nil {
		return s.listOverrides(ctx, groupID)
	}
	return nil, nil
}
func (s stubRepository) Create(ctx context.Context, mutation Mutation) (User, error) {
	if s.create != nil {
		return s.create(ctx, mutation)
	}
	return User{}, nil
}
func (s stubRepository) Update(ctx context.Context, id int, mutation Mutation) (User, error) {
	if s.update != nil {
		return s.update(ctx, id, mutation)
	}
	return User{}, nil
}
func (s stubRepository) UpdateBalance(ctx context.Context, id int, update BalanceUpdate) (User, error) {
	if s.updateBalance != nil {
		return s.updateBalance(ctx, id, update)
	}
	return User{}, nil
}
func (s stubRepository) Delete(ctx context.Context, id int) error {
	if s.delete != nil {
		return s.delete(ctx, id)
	}
	return nil
}
func (s stubRepository) ListBalanceLogs(ctx context.Context, userID int, page, pageSize int) ([]BalanceLog, int64, error) {
	if s.listBalanceLogs != nil {
		return s.listBalanceLogs(ctx, userID, page, pageSize)
	}
	return nil, 0, nil
}
func (s stubRepository) UpdateBalanceAlert(ctx context.Context, userID int, threshold float64) error {
	if s.updateBalanceAlert != nil {
		return s.updateBalanceAlert(ctx, userID, threshold)
	}
	return nil
}
func (s stubRepository) SetBalanceAlertNotified(ctx context.Context, userID int, notified bool) error {
	if s.setBalanceAlertNotified != nil {
		return s.setBalanceAlertNotified(ctx, userID, notified)
	}
	return nil
}
func (s stubRepository) ListAPIKeys(ctx context.Context, userID, page, pageSize int, _ time.Time) ([]APIKey, int64, error) {
	if s.listAPIKeys == nil {
		return nil, 0, nil
	}
	return s.listAPIKeys(ctx, userID, page, pageSize)
}
func (s stubRepository) GetAPIKeyName(ctx context.Context, keyID int) (string, error) {
	if s.getAPIKeyName != nil {
		return s.getAPIKeyName(ctx, keyID)
	}
	return "", nil
}
func (s stubRepository) GetAPIKeyInfo(ctx context.Context, keyID int) (APIKeyBrief, error) {
	if s.getAPIKeyInfo != nil {
		return s.getAPIKeyInfo(ctx, keyID)
	}
	return APIKeyBrief{}, nil
}
