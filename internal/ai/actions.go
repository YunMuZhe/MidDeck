// Package ai turns a natural-language request into one MidDeck UI action.
//
// The model only ever *proposes* an action: the frontend opens the matching form
// with the proposed values filled in, and nothing runs until the user confirms it
// there. Secrets never leave the machine — the model sees instance metadata only.
package ai

// Tool describes one action the model may propose. Schemas are plain JSON Schema
// and are shared by the Anthropic and OpenAI-compatible protocols.
type Tool struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}
func strList(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
}

var instanceID = str("id of the target instance, copied exactly from the instance list")

// Tools is the full action catalogue. Keep it in sync with frontend/src/assistant.ts.
var Tools = []Tool{
	{
		Name:        "open_instance",
		Description: "Open an existing instance, optionally on a tab. Use this when the user only wants to find or go to an instance.",
		Properties: map[string]any{
			"instance_id": instanceID,
			"tab":         enum("tab to open. kafka: topics|messages|groups; redis: keys|console|info; rabbitmq: queues|exchanges", "topics", "messages", "groups", "keys", "console", "info", "queues", "exchanges"),
		},
		Required: []string{"instance_id"},
	},
	{
		Name:        "new_connection",
		Description: "Prefill the form for adding a new connection (instance) to MidDeck.",
		Properties: map[string]any{
			"kind":        enum("middleware type", "kafka", "redis", "rabbitmq"),
			"name":        str("display name"),
			"project":     str("project / service the instance belongs to"),
			"env":         enum("environment", "dev", "test", "staging", "prod"),
			"addrs":       strList("host:port addresses: kafka bootstrap servers, redis nodes, or the rabbitmq AMQP address"),
			"redis_mode":  enum("redis only", "standalone", "cluster", "sentinel"),
			"redis_db":    integer("redis only: database number"),
			"mgmt_url":    str("rabbitmq only: management API URL, e.g. http://mq-1:15672"),
			"vhost":       str("rabbitmq only: virtual host"),
			"username":    str("username, if the user mentioned one (never invent passwords)"),
			"bastion_id":  str("id of a shared jump host from the jump host list, when the user says to go through one"),
			"aliases":     strList("other names people use for this instance"),
			"description": str("short description"),
		},
		Required: []string{"kind"},
	},
	{
		Name:        "kafka_create_topic",
		Description: "Prefill the create-topic form on a Kafka instance.",
		Properties: map[string]any{
			"instance_id":  instanceID,
			"name":         str("topic name"),
			"partitions":   integer("partition count"),
			"replication":  integer("replication factor"),
			"retention_ms": integer("retention.ms, e.g. 7 days = 604800000; omit if not mentioned"),
			"cleanup":      enum("cleanup.policy; omit if not mentioned", "delete", "compact", "compact,delete"),
		},
		Required: []string{"instance_id", "name"},
	},
	{
		Name:        "kafka_search_messages",
		Description: "Prefill the message search on a Kafka instance.",
		Properties: map[string]any{
			"instance_id": instanceID,
			"topic":       str("topic name"),
			"partition":   integer("partition, -1 for all"),
			"mode":        enum("where to start", "newest", "oldest", "offset", "timestamp"),
			"offset":      integer("start offset when mode=offset"),
			"since":       str("start time when mode=timestamp, RFC 3339 with timezone"),
			"limit":       integer("max messages"),
			"key":         str("exact message key filter"),
			"contains":    str("substring the message value must contain"),
		},
		Required: []string{"instance_id", "topic"},
	},
	{
		Name:        "kafka_reset_offsets",
		Description: "Prefill the reset-offsets dialog for a consumer group on a topic (used to skip stuck messages or replay).",
		Properties: map[string]any{
			"instance_id": instanceID,
			"group":       str("consumer group id"),
			"topic":       str("topic name"),
			"mode":        enum("target position", "earliest", "latest", "offset", "timestamp", "shift"),
			"value":       integer("offset for mode=offset, or delta for mode=shift (negative replays, positive skips)"),
			"since":       str("time for mode=timestamp, RFC 3339 with timezone"),
		},
		Required: []string{"instance_id", "group", "topic", "mode"},
	},
	{
		Name:        "redis_scan",
		Description: "Open the key browser on a Redis instance with a SCAN pattern.",
		Properties: map[string]any{
			"instance_id": instanceID,
			"pattern":     str("glob pattern, e.g. user:*"),
		},
		Required: []string{"instance_id", "pattern"},
	},
	{
		Name:        "redis_command",
		Description: "Type a command into the Redis console without running it.",
		Properties: map[string]any{
			"instance_id": instanceID,
			"command":     str("one redis command line, e.g. HGETALL session:42"),
		},
		Required: []string{"instance_id", "command"},
	},
	{
		Name:        "rabbit_create_queue",
		Description: "Prefill the create-queue form on a RabbitMQ instance.",
		Properties: map[string]any{
			"instance_id":   instanceID,
			"name":          str("queue name"),
			"type":          enum("queue type", "classic", "quorum", "stream"),
			"bind_exchange": str("exchange to bind to, if mentioned"),
			"routing_key":   str("binding routing key"),
		},
		Required: []string{"instance_id", "name"},
	},
	{
		Name:        "rabbit_publish",
		Description: "Prefill the publish-message form on a RabbitMQ instance.",
		Properties: map[string]any{
			"instance_id": instanceID,
			"exchange":    str("exchange name, empty string for the default exchange"),
			"routing_key": str("routing key (queue name for the default exchange)"),
			"payload":     str("message body"),
		},
		Required: []string{"instance_id", "routing_key"},
	},
	{
		Name:        "reply",
		Description: "Answer without opening anything: ask a clarifying question when the target is ambiguous, or explain why the request cannot be done in MidDeck.",
		Properties: map[string]any{
			"message":       str("short answer in the user's language"),
			"candidate_ids": strList("ids of instances that might be meant, best first"),
		},
		Required: []string{"message"},
	},
}

// schema returns the JSON Schema object for a tool's input.
func (t Tool) schema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           t.Properties,
		"required":             t.Required,
		"additionalProperties": false,
	}
}
