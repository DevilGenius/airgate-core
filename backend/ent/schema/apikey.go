package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// APIKey API 密钥
type APIKey struct {
	ent.Schema
}

func (APIKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("key_hint").Default(""),
		field.String("key_hash").NotEmpty().Sensitive(),
		field.String("key_encrypted").Optional().Sensitive(),
		field.JSON("ip_whitelist", []string{}).Optional(),
		field.JSON("ip_blacklist", []string{}).Optional(),
		field.Float("quota_usd").Default(0),
		field.Float("used_quota").Default(0).
			Comment("账面已用：累加 billed_cost（含 sell_rate markup）。end customer 看到的就是这个数字。"),
		field.Float("used_quota_actual").Default(0).
			Comment("真实成本已用：累加 actual_cost。reseller 用于成本核算/利润计算，end customer 不可见。"),
		field.Float("sell_rate").Default(0).Min(0).
			Comment("销售倍率：>0 时启用 reseller markup, billed_cost = base_cost × sell_rate；=0 表示不加价，billed_cost = actual_cost"),
		field.Time("expires_at").Optional().Nillable(),
		field.Enum("status").Values("active", "disabled").Default("active"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (APIKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("api_keys").Unique().Required(),
		edge.From("group", Group.Type).Ref("api_keys").Unique(),
		edge.To("usage_logs", UsageLog.Type),
	}
}
