package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Group 分组
type Group struct {
	ent.Schema
}

func (Group) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("platform").NotEmpty(),
		field.Float("rate_multiplier").Default(1.0),
		field.Bool("is_exclusive").Default(false),
		field.Enum("subscription_type").Values("standard", "subscription").Default("standard"),
		field.JSON("quotas", map[string]interface{}{}).Optional(),
		field.JSON("model_routing", map[string][]int64{}).Optional(),
		field.Int("sort_weight").Default(0),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Group) Edges() []ent.Edge {
	return []ent.Edge{
		// 分组关联的账号（多对多反向）
		edge.From("accounts", Account.Type).Ref("groups"),
		// 允许访问此专属分组的用户（多对多反向）
		edge.From("allowed_users", User.Type).Ref("allowed_groups"),
		edge.To("api_keys", APIKey.Type),
		edge.To("subscriptions", UserSubscription.Type),
		edge.To("usage_logs", UsageLog.Type),
	}
}
