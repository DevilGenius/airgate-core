package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MonitorRequestTrace is a content-addressed store for compressed final-error
// request diagnostics. Occurrences remain in monitor_request_events and refer
// to rows here by trace_hash.
type MonitorRequestTrace struct {
	ent.Schema
}

func (MonitorRequestTrace) Fields() []ent.Field {
	return []ent.Field{
		field.String("hash").NotEmpty().MaxLen(64).Unique(),
		field.Int("schema_version").Default(1),
		field.String("encoding").Default("gzip-json").MaxLen(32),
		field.Bytes("payload"),
		field.Int64("raw_size").Default(0),
		field.Int64("compressed_size").Default(0),
		field.Int64("seen_count").Default(1),
		field.Time("first_seen_at").Default(timeNow),
		field.Time("last_seen_at").Default(timeNow),
		field.Time("expires_at").Default(timeNow),
	}
}

func (MonitorRequestTrace) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("last_seen_at"),
		index.Fields("expires_at"),
	}
}

func (MonitorRequestTrace) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("monitor_request_trace")}
}
