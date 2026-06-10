package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MonitorEvent records short-lived operational monitoring events.
type MonitorEvent struct {
	ent.Schema
}

func (MonitorEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("type").
			Values("scheduler_error", "upstream_account_error", "plugin_error", "task_error", "system_error"),
		field.Enum("severity").
			Values("warning", "error", "critical").
			Default("warning"),
		field.Enum("status").
			Values("active", "resolved", "ignored").
			Default("active"),
		field.String("source").Default("").MaxLen(64),
		field.String("subject_type").Default("").MaxLen(64),
		field.String("subject_id").Default("").MaxLen(128),
		field.String("hash").NotEmpty().MaxLen(64),
		field.String("title").Default("").MaxLen(160),
		field.String("message").Default("").MaxLen(500),
		field.Int("account_id").Optional().Nillable(),
		field.String("account_name_snapshot").Default("").MaxLen(255),
		field.String("platform").Default("").MaxLen(128),
		field.String("plugin_id").Default("").MaxLen(128),
		field.String("task_type").Default("").MaxLen(128),
		field.String("error_code").Default("").MaxLen(64),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow),
		field.Time("resolved_at").Optional().Nillable(),
		field.Time("ignored_at").Optional().Nillable(),
		field.Time("auto_resolve_at").Optional().Nillable(),
		field.Time("expires_at").Default(timeNow),
		field.Time("last_notified_at").Optional().Nillable(),
		field.Time("next_notify_at").Optional().Nillable(),
		field.String("notify_error").Default("").MaxLen(500),
		field.JSON("detail", map[string]interface{}{}).
			Optional().
			Default(map[string]interface{}{}),
	}
}

func (MonitorEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "severity", "updated_at"),
		index.Fields("status", "type", "updated_at"),
		index.Fields("hash"),
		index.Fields("account_id", "status", "updated_at"),
		index.Fields("platform", "plugin_id", "status", "updated_at"),
		index.Fields("status", "auto_resolve_at"),
		index.Fields("expires_at"),
		index.Fields("status", "severity", "next_notify_at"),
	}
}
