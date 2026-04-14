package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Account 上游 AI 账户
type Account struct {
	ent.Schema
}

func (Account) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("platform").NotEmpty(),
		field.String("type").Default("").Optional(), // 账号类型，由插件定义（如 "apikey", "oauth"）
		field.JSON("credentials", map[string]string{}).Default(map[string]string{}),
		field.Enum("status").Values("active", "error", "disabled").Default("active"),
		field.Int("priority").Default(50).Min(0).Max(999),
		field.Int("max_concurrency").Default(10),
		field.Float("rate_multiplier").Default(1.0),
		field.String("error_msg").Default(""),
		field.Bool("upstream_is_pool").Default(false).
			Comment("上游是账号池：把 expired/disabled 降级为临时限流，避免池子耗尽时本地账号被永久标错"),
		field.Time("last_used_at").Optional().Nillable(),
		field.JSON("extra", map[string]interface{}{}).Optional().Default(map[string]interface{}{}). // 扩展配置：max_rpm, max_window_cost, max_sessions 等
														Comment("扩展配置（插件/调度器使用）"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Account) Edges() []ent.Edge {
	return []ent.Edge{
		// 账号所属分组（多对多）
		edge.To("groups", Group.Type),
		// 账号使用的代理
		edge.To("proxy", Proxy.Type).Unique(),
		edge.To("usage_logs", UsageLog.Type),
	}
}
