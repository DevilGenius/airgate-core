package billing

import (
	"fmt"
	"sync"
)

// ModelPrice 模型价格
type ModelPrice struct {
	InputPerToken  float64 // 每 token 输入价格（美元）
	OutputPerToken float64 // 每 token 输出价格（美元）
	CachePerToken  float64 // 每 token 缓存价格（美元）
}

// PriceManager 价格管理器
// 缓存模型价格信息，实际价格由插件提供，核心只做缓存
type PriceManager struct {
	mu     sync.RWMutex
	prices map[string]ModelPrice // key: "platform:model"
}

// NewPriceManager 创建价格管理器
func NewPriceManager() *PriceManager {
	return &PriceManager{
		prices: make(map[string]ModelPrice),
	}
}

// priceKey 生成价格缓存键
func priceKey(platform, model string) string {
	return fmt.Sprintf("%s:%s", platform, model)
}

// GetPrice 获取模型价格
func (m *PriceManager) GetPrice(platform, model string) (ModelPrice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	price, ok := m.prices[priceKey(platform, model)]
	return price, ok
}

// SetPrice 设置模型价格（由插件注册时调用）
func (m *PriceManager) SetPrice(platform, model string, price ModelPrice) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.prices[priceKey(platform, model)] = price
}

// SetPrices 批量设置价格
func (m *PriceManager) SetPrices(platform string, prices map[string]ModelPrice) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for model, price := range prices {
		m.prices[priceKey(platform, model)] = price
	}
}
