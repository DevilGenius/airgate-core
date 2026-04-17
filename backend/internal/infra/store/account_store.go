package store

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	"github.com/DouDOU-start/airgate-core/ent/predicate"
	entproxy "github.com/DouDOU-start/airgate-core/ent/proxy"
	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
	appaccount "github.com/DouDOU-start/airgate-core/internal/app/account"
)

// AccountStore 使用 Ent 实现账号仓储。
type AccountStore struct {
	db *ent.Client
}

// NewAccountStore 创建账号仓储。
func NewAccountStore(db *ent.Client) *AccountStore {
	return &AccountStore{db: db}
}

// List 查询账号列表。
func (s *AccountStore) List(ctx context.Context, filter appaccount.ListFilter) ([]appaccount.Account, int64, error) {
	query := s.db.Account.Query()

	if filter.Keyword != "" {
		query = query.Where(entaccount.NameContains(filter.Keyword))
	}
	if filter.Platform != "" {
		query = query.Where(entaccount.PlatformEQ(filter.Platform))
	}
	if filter.Status != "" {
		query = query.Where(entaccount.StatusEQ(entaccount.Status(filter.Status)))
	}
	if filter.AccountType != "" {
		query = query.Where(entaccount.TypeEQ(filter.AccountType))
	}
	if filter.GroupID != nil {
		query = query.Where(entaccount.HasGroupsWith(entgroup.ID(*filter.GroupID)))
	}
	if filter.ProxyID != nil {
		query = query.Where(entaccount.HasProxyWith(entproxy.IDEQ(*filter.ProxyID)))
	}

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	accounts, err := query.
		WithGroups().
		WithProxy().
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entaccount.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	return mapAccounts(accounts), int64(total), nil
}

// ListAll 查询符合筛选条件的全部账号（不分页，用于导出）。
func (s *AccountStore) ListAll(ctx context.Context, filter appaccount.ListFilter) ([]appaccount.Account, error) {
	query := s.db.Account.Query()

	if filter.Keyword != "" {
		query = query.Where(entaccount.NameContains(filter.Keyword))
	}
	if filter.Platform != "" {
		query = query.Where(entaccount.PlatformEQ(filter.Platform))
	}
	if filter.Status != "" {
		query = query.Where(entaccount.StatusEQ(entaccount.Status(filter.Status)))
	}
	if filter.AccountType != "" {
		query = query.Where(entaccount.TypeEQ(filter.AccountType))
	}
	if filter.GroupID != nil {
		query = query.Where(entaccount.HasGroupsWith(entgroup.ID(*filter.GroupID)))
	}
	if filter.ProxyID != nil {
		query = query.Where(entaccount.HasProxyWith(entproxy.IDEQ(*filter.ProxyID)))
	}
	if len(filter.IDs) > 0 {
		query = query.Where(entaccount.IDIn(filter.IDs...))
	}

	accounts, err := query.
		WithGroups().
		WithProxy().
		Order(ent.Desc(entaccount.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	return mapAccounts(accounts), nil
}

// Create 创建账号。
func (s *AccountStore) Create(ctx context.Context, input appaccount.CreateInput) (appaccount.Account, error) {
	builder := s.db.Account.Create().
		SetName(input.Name).
		SetPlatform(input.Platform).
		SetType(input.Type).
		SetCredentials(cloneCredentials(input.Credentials)).
		SetPriority(input.Priority).
		SetMaxConcurrency(input.MaxConcurrency).
		SetRateMultiplier(input.RateMultiplier).
		SetUpstreamIsPool(input.UpstreamIsPool)

	if len(input.GroupIDs) > 0 {
		builder = builder.AddGroupIDs(toIntSlice(input.GroupIDs)...)
	}
	if input.ProxyID != nil {
		builder = builder.SetProxyID(int(*input.ProxyID))
	}

	item, err := builder.Save(ctx)
	if err != nil {
		return appaccount.Account{}, err
	}

	return s.FindByID(ctx, item.ID, appaccount.LoadOptions{WithGroups: true, WithProxy: true})
}

// Update 更新账号。
func (s *AccountStore) Update(ctx context.Context, id int, input appaccount.UpdateInput) (appaccount.Account, error) {
	builder := s.db.Account.UpdateOneID(id)

	if input.Name != nil {
		builder = builder.SetName(*input.Name)
	}
	if input.Type != nil {
		builder = builder.SetType(*input.Type)
	}
	if input.Credentials != nil {
		builder = builder.SetCredentials(cloneCredentials(input.Credentials))
	}
	if input.Status != nil {
		builder = builder.SetStatus(entaccount.Status(*input.Status))
	}
	if input.Priority != nil {
		builder = builder.SetPriority(*input.Priority)
	}
	if input.MaxConcurrency != nil {
		builder = builder.SetMaxConcurrency(*input.MaxConcurrency)
	}
	if input.RateMultiplier != nil {
		builder = builder.SetRateMultiplier(*input.RateMultiplier)
	}
	if input.UpstreamIsPool != nil {
		builder = builder.SetUpstreamIsPool(*input.UpstreamIsPool)
	}
	if input.HasGroupIDs {
		builder = builder.ClearGroups()
		if len(input.GroupIDs) > 0 {
			builder = builder.AddGroupIDs(toIntSlice(input.GroupIDs)...)
		}
	}
	if input.HasProxyID {
		if input.ProxyID == nil {
			builder = builder.ClearProxy()
		} else {
			builder = builder.ClearProxy().SetProxyID(int(*input.ProxyID))
		}
	}

	if _, err := builder.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appaccount.Account{}, appaccount.ErrAccountNotFound
		}
		return appaccount.Account{}, err
	}

	return s.FindByID(ctx, id, appaccount.LoadOptions{WithGroups: true, WithProxy: true})
}

// Delete 删除账号。
func (s *AccountStore) Delete(ctx context.Context, id int) error {
	if err := s.db.Account.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appaccount.ErrAccountNotFound
		}
		return err
	}
	return nil
}

// FindByID 按 ID 查询账号。
func (s *AccountStore) FindByID(ctx context.Context, id int, opts appaccount.LoadOptions) (appaccount.Account, error) {
	query := s.db.Account.Query().Where(entaccount.IDEQ(id))
	if opts.WithGroups {
		query = query.WithGroups()
	}
	if opts.WithProxy {
		query = query.WithProxy()
	}

	item, err := query.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appaccount.Account{}, appaccount.ErrAccountNotFound
		}
		return appaccount.Account{}, err
	}
	return mapAccount(item), nil
}

// ListByPlatform 按平台查询账号。
func (s *AccountStore) ListByPlatform(ctx context.Context, platform string) ([]appaccount.Account, error) {
	accounts, err := s.db.Account.Query().
		Where(entaccount.PlatformEQ(platform)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return mapAccounts(accounts), nil
}

// FindUsageLogs 查询账号在指定时间范围内的使用记录。
func (s *AccountStore) FindUsageLogs(ctx context.Context, id int, startDate, endDate time.Time) ([]appaccount.UsageLog, error) {
	predicates := []predicate.UsageLog{
		entusagelog.HasAccountWith(entaccount.IDEQ(id)),
		entusagelog.CreatedAtGTE(startDate),
		entusagelog.CreatedAtLTE(endDate),
	}

	logs, err := s.db.UsageLog.Query().
		Where(predicates...).
		Select(
			entusagelog.FieldModel,
			entusagelog.FieldInputTokens,
			entusagelog.FieldOutputTokens,
			entusagelog.FieldTotalCost,
			entusagelog.FieldAccountCost,
			entusagelog.FieldActualCost,
			entusagelog.FieldDurationMs,
			entusagelog.FieldCreatedAt,
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]appaccount.UsageLog, 0, len(logs))
	for _, item := range logs {
		result = append(result, appaccount.UsageLog{
			Model:        item.Model,
			InputTokens:  int64(item.InputTokens),
			OutputTokens: int64(item.OutputTokens),
			TotalCost:    item.TotalCost,
			AccountCost:  item.AccountCost,
			ActualCost:   item.ActualCost,
			DurationMs:   item.DurationMs,
			CreatedAt:    item.CreatedAt,
		})
	}
	return result, nil
}

// BatchWindowStats 批量查询多个账号在 [startTime, now] 时间窗口内的聚合统计。
//
// 实现：一次 GROUP BY account_id 的聚合查询，覆盖所有传入的账号 ID。
// 返回的 map 只包含至少有一条 usage_log 的账号，其余账号由调用方按零值处理。
func (s *AccountStore) BatchWindowStats(ctx context.Context, accountIDs []int, startTime time.Time) (map[int]appaccount.AccountWindowStats, error) {
	if len(accountIDs) == 0 {
		return map[int]appaccount.AccountWindowStats{}, nil
	}

	var rows []struct {
		AccountID           int     `json:"account_usage_logs"`
		Count               int     `json:"count"`
		InputTokens         int64   `json:"input_tokens"`
		OutputTokens        int64   `json:"output_tokens"`
		CachedInputTokens   int64   `json:"cached_input_tokens"`
		CacheCreationTokens int64   `json:"cache_creation_tokens"`
		AccountCost         float64 `json:"account_cost"`
		ActualCost          float64 `json:"actual_cost"`
	}

	err := s.db.UsageLog.Query().
		Where(
			entusagelog.HasAccountWith(entaccount.IDIn(accountIDs...)),
			entusagelog.CreatedAtGTE(startTime),
		).
		GroupBy(entusagelog.AccountColumn).
		Aggregate(
			ent.Count(),
			ent.As(ent.Sum(entusagelog.FieldInputTokens), "input_tokens"),
			ent.As(ent.Sum(entusagelog.FieldOutputTokens), "output_tokens"),
			ent.As(ent.Sum(entusagelog.FieldCachedInputTokens), "cached_input_tokens"),
			ent.As(ent.Sum(entusagelog.FieldCacheCreationTokens), "cache_creation_tokens"),
			ent.As(ent.Sum(entusagelog.FieldAccountCost), "account_cost"),
			ent.As(ent.Sum(entusagelog.FieldActualCost), "actual_cost"),
		).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}

	result := make(map[int]appaccount.AccountWindowStats, len(rows))
	for _, r := range rows {
		if r.AccountID == 0 {
			continue
		}
		result[r.AccountID] = appaccount.AccountWindowStats{
			Requests:    int64(r.Count),
			Tokens:      r.InputTokens + r.OutputTokens + r.CachedInputTokens + r.CacheCreationTokens,
			AccountCost: r.AccountCost,
			UserCost:    r.ActualCost,
		}
	}
	return result, nil
}

// SaveCredentials 保存账号凭证。
func (s *AccountStore) SaveCredentials(ctx context.Context, id int, credentials map[string]string) error {
	if err := s.db.Account.UpdateOneID(id).
		SetCredentials(cloneCredentials(credentials)).
		Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appaccount.ErrAccountNotFound
		}
		return err
	}
	return nil
}

// MarkError 将账号标记为错误状态。
func (s *AccountStore) MarkError(ctx context.Context, id int, message string) error {
	if err := s.db.Account.UpdateOneID(id).
		SetStatus(entaccount.StatusError).
		SetErrorMsg(message).
		Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appaccount.ErrAccountNotFound
		}
		return err
	}
	return nil
}

// SetRateLimitResetAt 设置或清除账号的限流恢复时间。
// resetAt == nil 会清空字段；否则写入指定时间。
func (s *AccountStore) SetRateLimitResetAt(ctx context.Context, id int, resetAt *time.Time) error {
	builder := s.db.Account.UpdateOneID(id)
	if resetAt == nil {
		builder = builder.ClearRateLimitResetAt()
	} else {
		builder = builder.SetRateLimitResetAt(*resetAt)
	}
	if err := builder.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appaccount.ErrAccountNotFound
		}
		return err
	}
	return nil
}

func mapAccounts(accounts []*ent.Account) []appaccount.Account {
	result := make([]appaccount.Account, 0, len(accounts))
	for _, item := range accounts {
		result = append(result, mapAccount(item))
	}
	return result
}

func mapAccount(item *ent.Account) appaccount.Account {
	result := appaccount.Account{
		ID:             item.ID,
		Name:           item.Name,
		Platform:       item.Platform,
		Type:           item.Type,
		Credentials:    cloneCredentials(item.Credentials),
		Status:         item.Status.String(),
		Priority:       item.Priority,
		MaxConcurrency: item.MaxConcurrency,
		RateMultiplier: item.RateMultiplier,
		ErrorMsg:       item.ErrorMsg,
		UpstreamIsPool: item.UpstreamIsPool,
		Extra:          cloneAnyMap(item.Extra),
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}

	if item.LastUsedAt != nil {
		value := *item.LastUsedAt
		result.LastUsedAt = &value
	}
	if item.RateLimitResetAt != nil {
		value := *item.RateLimitResetAt
		result.RateLimitResetAt = &value
	}
	if item.Edges.Proxy != nil {
		result.Proxy = &appaccount.Proxy{
			ID:       item.Edges.Proxy.ID,
			Protocol: string(item.Edges.Proxy.Protocol),
			Address:  item.Edges.Proxy.Address,
			Port:     item.Edges.Proxy.Port,
			Username: item.Edges.Proxy.Username,
			Password: item.Edges.Proxy.Password,
		}
	}
	for _, relatedGroup := range item.Edges.Groups {
		result.GroupIDs = append(result.GroupIDs, int64(relatedGroup.ID))
	}

	return result
}

func toIntSlice(values []int64) []int {
	result := make([]int, 0, len(values))
	for _, value := range values {
		result = append(result, int(value))
	}
	return result
}

func cloneCredentials(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(input map[string]interface{}) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

var _ appaccount.Repository = (*AccountStore)(nil)
