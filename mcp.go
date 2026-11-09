package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/YunMuZhe/MidDeck/internal/driver/redis"
	"github.com/YunMuZhe/MidDeck/internal/mcp"
)

// runMCP serves MidDeck's saved connections to AI coding tools over stdio:
//
//	MidDeck mcp
//
// Everything exposed is read-only; changes stay in the GUI, where they get
// confirmation dialogs, read-only enforcement and the audit log.
func runMCP() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a := NewApp()
	if err := a.init(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "middeck mcp:", err)
		return 1
	}
	defer a.shutdown(ctx)
	s := &mcp.Server{
		Name:    "middeck",
		Version: version,
		Instructions: "MidDeck gives read-only access to the user's saved Kafka, Redis and RabbitMQ connections, " +
			"including ones behind SSH jump hosts. Start with list_instances, then pass an instance id or name to the other tools. " +
			"Nothing here can modify data; for changes, tell the user to use the MidDeck app.",
		Tools: mcpTools(a),
	}
	if err := s.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "middeck mcp:", err)
		return 1
	}
	return 0
}

// resolve finds an instance by id, exact name, alias, or a unique partial match.
func (a *App) resolve(ref string) (config.Instance, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return config.Instance{}, errors.New("instance is required; call list_instances first")
	}
	list := a.store.List()
	low := strings.ToLower(ref)
	var partial []config.Instance
	for _, in := range list {
		if in.ID == ref || strings.EqualFold(in.Name, ref) {
			return in, nil
		}
		for _, al := range in.Aliases {
			if strings.EqualFold(al, ref) {
				return in, nil
			}
		}
		if strings.Contains(strings.ToLower(in.Name), low) {
			partial = append(partial, in)
		}
	}
	if len(partial) == 1 {
		return partial[0], nil
	}
	if len(partial) > 1 {
		var names []string
		for _, in := range partial {
			names = append(names, fmt.Sprintf("%s (%s)", in.Name, in.ID))
		}
		return config.Instance{}, fmt.Errorf("%q matches several instances: %s", ref, strings.Join(names, ", "))
	}
	return config.Instance{}, fmt.Errorf("no instance matches %q; call list_instances", ref)
}

func mcpTools(a *App) []mcp.Tool {
	inst := map[string]any{"type": "string", "description": "instance id, name or alias (see list_instances)"}
	s := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	n := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	need := func(kind config.Kind, args map[string]any) (config.Instance, error) {
		in, err := a.resolve(mcp.Str(args, "instance"))
		if err == nil && in.Kind != kind {
			err = fmt.Errorf("%s is a %s instance, not %s", in.Name, in.Kind, kind)
		}
		return in, err
	}
	filter := func(name, f string) bool {
		return f == "" || strings.Contains(strings.ToLower(name), strings.ToLower(f))
	}

	return []mcp.Tool{
		{
			Name: "list_instances", Title: "List connections",
			Description: "List the Kafka, Redis and RabbitMQ connections saved in MidDeck. Optional filters narrow the list.",
			Properties: map[string]any{
				"query": s("words to match against name, project, tags, aliases and description"),
				"kind":  map[string]any{"type": "string", "enum": []string{"kafka", "redis", "rabbitmq"}},
				"env":   s("environment, e.g. prod"),
			},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				q := strings.Fields(strings.ToLower(mcp.Str(args, "query")))
				type row struct {
					ID          string   `json:"id"`
					Name        string   `json:"name"`
					Kind        string   `json:"kind"`
					Project     string   `json:"project,omitempty"`
					Env         string   `json:"env,omitempty"`
					Tags        []string `json:"tags,omitempty"`
					Aliases     []string `json:"aliases,omitempty"`
					Description string   `json:"description,omitempty"`
					Addrs       []string `json:"addrs,omitempty"`
					ReadOnly    bool     `json:"readOnly,omitempty"`
					ViaSSH      int      `json:"sshHops,omitempty"`
				}
				out := []row{}
				for _, in := range a.store.List() {
					if k := mcp.Str(args, "kind"); k != "" && string(in.Kind) != k {
						continue
					}
					if e := mcp.Str(args, "env"); e != "" && !strings.EqualFold(in.Env, e) {
						continue
					}
					hay := strings.ToLower(strings.Join(append(append([]string{in.Name, in.Project, in.Env, in.Description}, in.Tags...), in.Aliases...), " "))
					ok := true
					for _, w := range q {
						ok = ok && strings.Contains(hay, w)
					}
					if !ok {
						continue
					}
					addrs := in.Addrs
					if in.MgmtURL != "" {
						addrs = append(addrs, in.MgmtURL)
					}
					out = append(out, row{in.ID, in.Name, string(in.Kind), in.Project, in.Env, in.Tags, in.Aliases, in.Description, addrs, in.ReadOnly, len(in.Network.SSH)})
				}
				return out, nil
			},
		},
		{
			Name: "server_info", Title: "Server version and compatibility",
			Description: "Connect to an instance (through its SSH jump hosts if any) and report the server version, mode and MidDeck's compatibility verdict. On failure, returns the layered connection diagnostics.",
			Properties:  map[string]any{"instance": inst}, Required: []string{"instance"},
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				in, err := a.resolve(mcp.Str(args, "instance"))
				if err != nil {
					return nil, err
				}
				r, err := a.Connect(in.ID)
				if err != nil {
					return map[string]any{"error": err.Error(), "diagnostics": r.Report}, nil
				}
				return r.Info, nil
			},
		},
		{
			Name: "kafka_list_topics", Title: "List Kafka topics",
			Description: "List topics with partition and replication counts.",
			Properties:  map[string]any{"instance": inst, "filter": s("substring of the topic name")}, Required: []string{"instance"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindKafka, args)
				if err != nil {
					return nil, err
				}
				all, err := a.KafkaTopics(in.ID)
				out := []kafka.Topic{}
				for _, t := range all {
					if filter(t.Name, mcp.Str(args, "filter")) {
						out = append(out, t)
					}
				}
				return out, err
			},
		},
		{
			Name: "kafka_describe_topic", Title: "Describe a Kafka topic",
			Description: "Partitions (leader, replicas, ISR, start/end offsets), message count and configs of a topic.",
			Properties:  map[string]any{"instance": inst, "topic": s("topic name")}, Required: []string{"instance", "topic"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindKafka, args)
				if err != nil {
					return nil, err
				}
				return a.KafkaDescribeTopic(in.ID, mcp.Str(args, "topic"))
			},
		},
		{
			Name: "kafka_fetch_messages", Title: "Read Kafka messages",
			Description: "Read messages without joining a consumer group (no offsets are committed). Values are returned as text.",
			Properties: map[string]any{
				"instance": inst, "topic": s("topic name"),
				"partition": n("partition, -1 (default) for all"),
				"mode":      map[string]any{"type": "string", "enum": []string{"newest", "oldest", "offset", "timestamp"}, "description": "where to start, default newest"},
				"offset":    n("start offset for mode=offset"),
				"since":     s("RFC 3339 time for mode=timestamp"),
				"limit":     n("max messages, default 20, at most 500"),
				"key":       s("only messages with exactly this key"),
				"contains":  s("only messages whose value contains this text"),
			},
			Required: []string{"instance", "topic"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindKafka, args)
				if err != nil {
					return nil, err
				}
				r := kafka.FetchReq{
					Topic: mcp.Str(args, "topic"), Partition: int32(mcp.Int(args, "partition", -1)), Mode: mcp.Str(args, "mode"),
					Offset: mcp.Int(args, "offset", 0), Limit: int(mcp.Int(args, "limit", 20)),
					KeyFilter: mcp.Str(args, "key"), Contains: mcp.Str(args, "contains"),
				}
				if r.Mode == "" {
					r.Mode = "newest"
				}
				if r.Limit > 500 {
					r.Limit = 500
				}
				if since := mcp.Str(args, "since"); since != "" {
					ts, err := time.Parse(time.RFC3339, since)
					if err != nil {
						return nil, fmt.Errorf("since must be RFC 3339: %w", err)
					}
					r.Mode, r.Timestamp = "timestamp", ts.UnixMilli()
				}
				return a.KafkaFetch(in.ID, r)
			},
		},
		{
			Name: "kafka_list_groups", Title: "List consumer groups",
			Description: "Consumer groups with state, member count, total lag and subscribed topics.",
			Properties:  map[string]any{"instance": inst, "filter": s("substring of the group id or a topic name")}, Required: []string{"instance"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindKafka, args)
				if err != nil {
					return nil, err
				}
				all, err := a.KafkaGroups(in.ID)
				out := []kafka.GroupSummary{}
				f := mcp.Str(args, "filter")
				for _, g := range all {
					match := filter(g.Group, f)
					for _, t := range g.Topics {
						match = match || filter(t, f)
					}
					if match {
						out = append(out, g)
					}
				}
				return out, err
			},
		},
		{
			Name: "kafka_describe_group", Title: "Describe a consumer group",
			Description: "Per-partition committed offset, end offset, lag and assigned member of a consumer group.",
			Properties:  map[string]any{"instance": inst, "group": s("consumer group id")}, Required: []string{"instance", "group"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindKafka, args)
				if err != nil {
					return nil, err
				}
				return a.KafkaDescribeGroup(in.ID, mcp.Str(args, "group"))
			},
		},
		{
			Name: "redis_info", Title: "Redis server info",
			Description: "Selected INFO fields: version, memory, clients, ops/sec, keyspace, hit rate inputs, replication role.",
			Properties:  map[string]any{"instance": inst}, Required: []string{"instance"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRedis, args)
				if err != nil {
					return nil, err
				}
				return a.RedisOverview(in.ID)
			},
		},
		{
			Name: "redis_scan", Title: "Find Redis keys",
			Description: "SCAN for keys matching a glob pattern (never KEYS). Returns key, type and TTL.",
			Properties:  map[string]any{"instance": inst, "pattern": s("glob pattern, e.g. user:42:*"), "count": n("max keys, default 200, at most 2000")},
			Required:    []string{"instance", "pattern"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRedis, args)
				if err != nil {
					return nil, err
				}
				limit := mcp.Int(args, "count", 200)
				if limit > 2000 {
					limit = 2000
				}
				keys := []redis.KeyItem{}
				var cursor uint64
				for i := 0; i < 50 && int64(len(keys)) < limit; i++ {
					r, err := a.RedisScan(in.ID, mcp.Str(args, "pattern"), cursor, 500)
					if err != nil {
						return nil, err
					}
					keys = append(keys, r.Keys...)
					if r.Done {
						break
					}
					cursor = r.Cursor
				}
				if int64(len(keys)) > limit {
					keys = keys[:limit]
				}
				return keys, nil
			},
		},
		{
			Name: "redis_get", Title: "Read a Redis key",
			Description: "Value of a key of any type (string, hash, list, set, zset, stream), with TTL and size. Large collections are truncated.",
			Properties:  map[string]any{"instance": inst, "key": s("exact key")}, Required: []string{"instance", "key"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRedis, args)
				if err != nil {
					return nil, err
				}
				return a.RedisGet(in.ID, mcp.Str(args, "key"))
			},
		},
		{
			Name: "redis_command", Title: "Run a read-only Redis command",
			Description: "Run one read-only command such as HGETALL, ZRANGE, TTL, XINFO or MEMORY USAGE. Writes and blocking commands (KEYS, FLUSHALL, …) are refused.",
			Properties:  map[string]any{"instance": inst, "command": s("one command line, arguments quoted as in redis-cli")},
			Required:    []string{"instance", "command"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRedis, args)
				if err != nil {
					return nil, err
				}
				line := mcp.Str(args, "command")
				if !redis.IsReadCmd(line) || redis.IsDangerous(line) {
					return nil, errors.New("only read-only, non-blocking commands are allowed over MCP; use the MidDeck app for anything else")
				}
				return a.RedisExec(in.ID, line, false)
			},
		},
		{
			Name: "redis_slowlog", Title: "Redis slow log",
			Description: "Most recent SLOWLOG entries.",
			Properties:  map[string]any{"instance": inst, "count": n("entries, default 20")}, Required: []string{"instance"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRedis, args)
				if err != nil {
					return nil, err
				}
				return a.RedisSlowLog(in.ID, mcp.Int(args, "count", 20))
			},
		},
		{
			Name: "rabbit_list_queues", Title: "List RabbitMQ queues",
			Description: "Queues with type, ready/unacked message counts and consumers.",
			Properties:  map[string]any{"instance": inst, "filter": s("substring of the queue name")}, Required: []string{"instance"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRabbitMQ, args)
				if err != nil {
					return nil, err
				}
				all, err := a.RabbitQueues(in.ID)
				type row struct {
					Name      string `json:"name"`
					Type      string `json:"type"`
					State     string `json:"state"`
					Ready     int64  `json:"ready"`
					Unacked   int64  `json:"unacked"`
					Consumers int64  `json:"consumers"`
				}
				out := []row{}
				for _, q := range all {
					if filter(q.Name, mcp.Str(args, "filter")) {
						out = append(out, row{q.Name, q.Type, q.State, q.Ready, q.Unacked, q.Consumers})
					}
				}
				return out, err
			},
		},
		{
			Name: "rabbit_list_exchanges", Title: "List RabbitMQ exchanges",
			Properties: map[string]any{"instance": inst}, Required: []string{"instance"},
			Description: "Exchanges with type and durability.",
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRabbitMQ, args)
				if err != nil {
					return nil, err
				}
				return a.RabbitExchanges(in.ID)
			},
		},
		{
			Name: "rabbit_queue_bindings", Title: "Queue bindings",
			Description: "Bindings (source exchange and routing key) of a queue.",
			Properties:  map[string]any{"instance": inst, "queue": s("queue name")}, Required: []string{"instance", "queue"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRabbitMQ, args)
				if err != nil {
					return nil, err
				}
				return a.RabbitQueueBindings(in.ID, mcp.Str(args, "queue"))
			},
		},
		{
			Name: "rabbit_peek", Title: "Peek RabbitMQ messages",
			Description: "Look at the first messages of a queue. They are put back (requeued), so they stay in the queue but are marked redelivered.",
			Properties:  map[string]any{"instance": inst, "queue": s("queue name"), "count": n("messages, default 10, at most 100")},
			Required:    []string{"instance", "queue"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := need(config.KindRabbitMQ, args)
				if err != nil {
					return nil, err
				}
				c := mcp.Int(args, "count", 10)
				if c > 100 {
					c = 100
				}
				return a.RabbitPeek(in.ID, mcp.Str(args, "queue"), int(c))
			},
		},
	}
}

// MCPInfo tells the UI how to register MidDeck as an MCP server.
type MCPInfo struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func (a *App) MCPInfo() MCPInfo {
	exe, err := os.Executable()
	if err != nil {
		exe = "MidDeck"
	}
	return MCPInfo{Command: exe, Args: []string{"mcp"}}
}
