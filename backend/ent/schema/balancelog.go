package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// BalanceLog 余额变更日志
type BalanceLog struct {
	ent.Schema
}

func (BalanceLog) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("action").Values("add", "subtract", "set"),
		field.Float("amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("before_balance").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("after_balance").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.String("remark").Default(""),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (BalanceLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("balance_logs").Unique().Required(),
	}
}
