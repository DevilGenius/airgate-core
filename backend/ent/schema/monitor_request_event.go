package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MonitorRequestEvent records request-level monitoring events. It is append-only
// and intentionally independent from system monitor resolve/notification state.
type MonitorRequestEvent struct {
	ent.Schema
}

func (MonitorRequestEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("type").Default("").MaxLen(64),
		field.Enum("severity").
			Values("info", "warning").
			Default("info"),
		field.String("source").Default("").MaxLen(64),
		field.String("hash").NotEmpty().MaxLen(64),
		field.String("fingerprint").Default("").MaxLen(128),
		field.String("title").Default("").MaxLen(160),
		field.String("message").Default("").MaxLen(500),
		field.String("request_id").Default("").MaxLen(128),
		field.Int("api_key_id").Optional().Nillable(),
		field.String("api_key_name_snapshot").Default("").MaxLen(255),
		field.Int("user_id").Optional().Nillable(),
		field.String("user_email_snapshot").Default("").MaxLen(255),
		field.Int("group_id").Optional().Nillable(),
		field.Int("account_id").Optional().Nillable(),
		field.String("account_name_snapshot").Default("").MaxLen(255),
		field.String("platform").Default("").MaxLen(128),
		field.String("plugin_id").Default("").MaxLen(128),
		field.String("method").Default("").MaxLen(64),
		field.String("endpoint").Default("").MaxLen(256),
		field.String("model").Default("").MaxLen(128),
		field.Int("http_status").Optional().Nillable(),
		field.Int("upstream_status").Optional().Nillable(),
		field.String("error_code").Default("").MaxLen(64),
		field.Int64("duration_ms").Default(0),
		field.Time("created_at").Default(timeNow),
		field.Time("expires_at").Default(timeNow),
		field.JSON("detail", map[string]interface{}{}).
			Optional().
			Default(map[string]interface{}{}),
	}
}

func (MonitorRequestEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
		index.Fields("expires_at"),
		index.Fields("api_key_id", "created_at"),
		index.Fields("endpoint", "created_at"),
		index.Fields("http_status", "created_at"),
		index.Fields("error_code", "created_at"),
		index.Fields("request_id"),
		index.Fields("hash"),
	}
}
