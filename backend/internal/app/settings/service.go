package settings

import (
	"context"

	sdk "github.com/DouDOU-start/airgate-sdk"
)

// Service 提供设置域用例编排。
type Service struct {
	repo Repository
}

// NewService 创建设置服务。
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// List 查询设置列表。
func (s *Service) List(ctx context.Context, group string) ([]Setting, error) {
	items, err := s.repo.List(ctx, group)
	if err != nil {
		sdk.LoggerFromContext(ctx).Error("settings_load_failed",
			"group", group,
			sdk.LogFieldError, err)
	}
	return items, err
}

// Update 批量更新设置。
func (s *Service) Update(ctx context.Context, items []ItemInput) error {
	logger := sdk.LoggerFromContext(ctx)
	cloned := make([]ItemInput, 0, len(items))
	keys := make([]string, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, ItemInput{
			Key:   item.Key,
			Value: item.Value,
			Group: item.Group,
		})
		keys = append(keys, item.Key)
	}
	if err := s.repo.UpsertMany(ctx, cloned); err != nil {
		logger.Error("settings_updated_failed",
			"keys", keys,
			sdk.LogFieldError, err)
		return err
	}
	// 仅打印 key 列表；values 可能含敏感配置（API key、密钥等），绝不日志化。
	logger.Info("settings_updated", "keys", keys, "count", len(cloned))
	return nil
}
