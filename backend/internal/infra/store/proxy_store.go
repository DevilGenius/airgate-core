package store

import (
	"context"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	entproxy "github.com/DevilGenius/airgate-core/ent/proxy"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
)

// ProxyStore 使用 Ent 实现代理仓储。
type ProxyStore struct {
	db *ent.Client
}

// NewProxyStore 创建代理仓储。
func NewProxyStore(db *ent.Client) *ProxyStore {
	return &ProxyStore{db: db}
}

// List 查询代理列表。
func (s *ProxyStore) List(ctx context.Context, filter appproxy.ListFilter) ([]appproxy.Proxy, int64, error) {
	query := s.db.Proxy.Query()
	if filter.Keyword != "" {
		query = query.Where(entproxy.NameContains(filter.Keyword))
	}
	if filter.Status != "" {
		query = query.Where(entproxy.StatusEQ(entproxy.Status(filter.Status)))
	}

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	items, err := query.
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Order(ent.Desc(entproxy.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	assigned, err := s.assignedSlots(ctx, items)
	if err != nil {
		return nil, 0, err
	}
	result := mapProxyList(items)
	for index := range result {
		result[index].AssignedSlots = assigned[result[index].ID]
	}
	return result, int64(total), nil
}

func (s *ProxyStore) assignedSlots(ctx context.Context, proxies []*ent.Proxy) (map[int]int, error) {
	result := make(map[int]int, len(proxies))
	ids := make([]int, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy.Mode == entproxy.ModeGroup {
			ids = append(ids, proxy.ID)
		}
	}
	if len(ids) == 0 {
		return result, nil
	}
	builder := entsql.Dialect(s.db.Driver().Dialect())
	table := builder.Table(entaccount.Table)
	selector := builder.
		Select(table.C(entaccount.ProxyColumn), "COUNT(DISTINCT "+table.C(entaccount.FieldProxySlot)+")").
		From(table).
		Where(entsql.And(
			entsql.InInts(table.C(entaccount.ProxyColumn), ids...),
			entsql.NotNull(table.C(entaccount.FieldProxySlot)),
		)).
		GroupBy(table.C(entaccount.ProxyColumn))
	query, args := selector.Query()
	var rows entsql.Rows
	if err := s.db.Driver().Query(ctx, query, args, &rows); err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var proxyID, count int
		if err := rows.Scan(&proxyID, &count); err != nil {
			return nil, err
		}
		result[proxyID] = count
	}
	return result, rows.Err()
}

// FindByID 按 ID 查询代理。
func (s *ProxyStore) FindByID(ctx context.Context, id int) (appproxy.Proxy, error) {
	item, err := s.db.Proxy.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return appproxy.Proxy{}, appproxy.ErrProxyNotFound
		}
		return appproxy.Proxy{}, err
	}
	return mapProxy(item), nil
}

// Create 创建代理。
func (s *ProxyStore) Create(ctx context.Context, input appproxy.CreateInput) (appproxy.Proxy, error) {
	input.Mode = appproxy.NormalizeMode(input.Mode)
	if err := appproxy.ValidateConfig(input.Mode, input.SlotStart, input.SlotEnd); err != nil {
		return appproxy.Proxy{}, err
	}
	if input.Mode == appproxy.ModeSingle {
		input.SlotStart, input.SlotEnd = 0, 0
	}
	builder := s.db.Proxy.Create().
		SetName(input.Name).
		SetMode(entproxy.Mode(input.Mode)).
		SetProtocol(entproxy.Protocol(input.Protocol)).
		SetAddress(input.Address).
		SetPort(input.Port).
		SetSlotStart(input.SlotStart).
		SetSlotEnd(input.SlotEnd)

	if input.Username != "" {
		builder = builder.SetUsername(input.Username)
	}
	if input.Password != "" {
		builder = builder.SetPassword(input.Password)
	}

	item, err := builder.Save(ctx)
	if err != nil {
		return appproxy.Proxy{}, err
	}
	return mapProxy(item), nil
}

// Update 更新代理。
func (s *ProxyStore) Update(ctx context.Context, id int, input appproxy.UpdateInput) (appproxy.Proxy, error) {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return appproxy.Proxy{}, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := lockProxyForSlot(ctx, tx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return appproxy.Proxy{}, appproxy.ErrProxyNotFound
		}
		return appproxy.Proxy{}, err
	}
	mode := current.Mode.String()
	if input.Mode != nil {
		mode = appproxy.NormalizeMode(*input.Mode)
	}
	slotStart, slotEnd := current.SlotStart, current.SlotEnd
	if input.SlotStart != nil {
		slotStart = *input.SlotStart
	}
	if input.SlotEnd != nil {
		slotEnd = *input.SlotEnd
	}
	if err := appproxy.ValidateConfig(mode, slotStart, slotEnd); err != nil {
		return appproxy.Proxy{}, err
	}
	if mode == appproxy.ModeSingle {
		slotStart, slotEnd = 0, 0
	}
	if input.Mode != nil && mode != current.Mode.String() {
		if assigned, countErr := tx.Account.Query().
			Where(entaccount.HasProxyWith(entproxy.IDEQ(id)), entaccount.DeletedAtIsNil()).
			Count(ctx); countErr != nil {
			return appproxy.Proxy{}, countErr
		} else if assigned > 0 {
			return appproxy.Proxy{}, appproxy.ErrProxyGroupHasAccounts
		}
	}
	if mode == appproxy.ModeGroup && (input.SlotStart != nil || input.SlotEnd != nil) {
		outside, rangeErr := tx.Account.Query().
			Where(
				entaccount.HasProxyWith(entproxy.IDEQ(id)),
				entaccount.ProxySlotNotNil(),
				entaccount.Or(
					entaccount.ProxySlotLT(slotStart),
					entaccount.ProxySlotGT(slotEnd),
				),
			).
			Exist(ctx)
		if rangeErr != nil {
			return appproxy.Proxy{}, rangeErr
		}
		if outside {
			return appproxy.Proxy{}, appproxy.ErrProxySlotRangeInUse
		}
	}
	builder := tx.Proxy.UpdateOneID(id)

	if input.Name != nil {
		builder = builder.SetName(*input.Name)
	}
	if input.Mode != nil {
		builder = builder.SetMode(entproxy.Mode(mode))
	}
	if input.Protocol != nil {
		builder = builder.SetProtocol(entproxy.Protocol(*input.Protocol))
	}
	if input.Address != nil {
		builder = builder.SetAddress(*input.Address)
	}
	if input.Port != nil {
		builder = builder.SetPort(*input.Port)
	}
	if input.Username != nil {
		builder = builder.SetUsername(*input.Username)
	}
	if input.Password != nil {
		builder = builder.SetPassword(*input.Password)
	}
	if input.SlotStart != nil || input.SlotEnd != nil || input.Mode != nil {
		builder = builder.SetSlotStart(slotStart).SetSlotEnd(slotEnd)
	}
	if input.Status != nil {
		builder = builder.SetStatus(entproxy.Status(*input.Status))
	}

	item, err := builder.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appproxy.Proxy{}, appproxy.ErrProxyNotFound
		}
		return appproxy.Proxy{}, err
	}
	result := mapProxy(item)
	if err := tx.Commit(); err != nil {
		return appproxy.Proxy{}, err
	}
	return result, nil
}

// Delete 删除代理。
func (s *ProxyStore) Delete(ctx context.Context, id int) error {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := lockProxyForSlot(ctx, tx, id); err != nil {
		if ent.IsNotFound(err) {
			return appproxy.ErrProxyNotFound
		}
		return err
	}
	if _, err := tx.Account.Update().
		Where(entaccount.HasProxyWith(entproxy.IDEQ(id))).
		ClearProxy().
		ClearProxySlot().
		Save(ctx); err != nil {
		return err
	}
	if err := tx.Proxy.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appproxy.ErrProxyNotFound
		}
		return err
	}
	return tx.Commit()
}

func mapProxyList(items []*ent.Proxy) []appproxy.Proxy {
	result := make([]appproxy.Proxy, 0, len(items))
	for _, item := range items {
		result = append(result, mapProxy(item))
	}
	return result
}

func mapProxy(item *ent.Proxy) appproxy.Proxy {
	return appproxy.Proxy{
		ID:        item.ID,
		Name:      item.Name,
		Mode:      item.Mode.String(),
		Protocol:  item.Protocol.String(),
		Address:   item.Address,
		Port:      item.Port,
		Username:  item.Username,
		Password:  item.Password,
		SlotStart: item.SlotStart,
		SlotEnd:   item.SlotEnd,
		Status:    item.Status.String(),
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}
