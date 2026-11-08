// Package redis implements the Redis driver on top of go-redis.
package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/compat"
	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver"
	goredis "github.com/redis/go-redis/v9"
)

// Session is an open Redis connection (standalone, sentinel or cluster).
type Session struct {
	inst    config.Instance
	cl      goredis.UniversalClient
	cluster *goredis.ClusterClient
}

// dangerous commands that need explicit confirmation in the console.
var dangerous = map[string]bool{
	"flushall": true, "flushdb": true, "keys": true, "shutdown": true, "debug": true,
	"config": true, "del": true, "unlink": true, "rename": true, "migrate": true, "restore": true,
	"script": true, "function": true, "monitor": true, "save": true, "bgsave": true, "replicaof": true, "slaveof": true,
	"cluster": true, "acl": true, "eval": true, "evalsha": true, "expire": true, "persist": true,
}

var readCmds = map[string]bool{
	"get": true, "mget": true, "getrange": true, "strlen": true, "exists": true, "type": true, "ttl": true, "pttl": true,
	"hget": true, "hmget": true, "hgetall": true, "hkeys": true, "hvals": true, "hlen": true, "hexists": true, "hscan": true, "hstrlen": true,
	"lrange": true, "llen": true, "lindex": true, "lpos": true,
	"smembers": true, "scard": true, "sismember": true, "smismember": true, "sscan": true, "srandmember": true,
	"zrange": true, "zrangebyscore": true, "zrevrange": true, "zrevrangebyscore": true, "zcard": true, "zscore": true, "zrank": true, "zrevrank": true, "zcount": true, "zscan": true,
	"xrange": true, "xrevrange": true, "xlen": true, "xinfo": true, "xpending": true,
	"scan": true, "dbsize": true, "info": true, "ping": true, "echo": true, "time": true, "memory": true, "object": true,
	"slowlog": true, "command": true, "pubsub": true, "bitcount": true, "getbit": true, "pfcount": true,
	"geopos": true, "geodist": true, "geohash": true, "json.get": true, "role": true, "lastsave": true,
}

// IsReadCmd reports whether a console command only reads data.
func IsReadCmd(cmd string) bool {
	f := strings.Fields(cmd)
	return len(f) > 0 && readCmds[strings.ToLower(f[0])]
}

// IsDangerous reports whether a console command needs confirmation.
func IsDangerous(cmd string) bool {
	f := strings.Fields(cmd)
	return len(f) > 0 && dangerous[strings.ToLower(f[0])]
}

func Open(ctx context.Context, inst config.Instance, rawDial driver.DialFunc, rep *diag.Report) (*Session, error) {
	tlsCfg, err := driver.TLSConfig(inst.TLS)
	if err != nil {
		rep.Add(diag.Step{Name: "TLS 配置", Status: diag.Fail, Detail: err.Error()})
		return nil, err
	}
	if len(inst.Addrs) == 0 {
		err := errors.New("至少需要一个地址")
		rep.Add(diag.Step{Name: "配置检查", Status: diag.Fail, Detail: err.Error()})
		return nil, err
	}
	for _, addr := range inst.Addrs {
		if err := rep.Run("TCP 连通 "+addr, func() (string, error) {
			c, err := rawDial(ctx, "tcp", addr)
			if err != nil {
				return "", err
			}
			c.Close()
			return "", nil
		}, driver.NetHint); err != nil && inst.RedisMode != "cluster" && inst.RedisMode != "sentinel" {
			return nil, err
		}
	}
	dial := driver.WithTLS(rawDial, tlsCfg)
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) { return dial(ctx, network, addr) }

	s := &Session{inst: inst}
	switch inst.RedisMode {
	case "cluster":
		s.cluster = goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs: inst.Addrs, Username: inst.Auth.Username, Password: inst.Auth.Password,
			Dialer: dialer, DialTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
			// Node addresses announced by CLUSTER SLOTS go through the same dialer,
			// so Network.AddrMap can rewrite unreachable ones.
		})
		s.cl = s.cluster
	case "sentinel":
		s.cl = goredis.NewFailoverClient(&goredis.FailoverOptions{
			MasterName: inst.MasterSet, SentinelAddrs: inst.Addrs,
			Username: inst.Auth.Username, Password: inst.Auth.Password, DB: inst.RedisDB,
			Dialer: dialer, DialTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		})
	default:
		s.cl = goredis.NewClient(&goredis.Options{
			Addr: inst.Addrs[0], Username: inst.Auth.Username, Password: inst.Auth.Password, DB: inst.RedisDB,
			Dialer: dialer, DialTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		})
	}

	if err := rep.Run("PING（含认证）", func() (string, error) {
		return s.cl.Ping(ctx).Result()
	}, driver.NetHint); err != nil {
		s.Close()
		return nil, err
	}
	_ = rep.Run("服务端版本识别", func() (string, error) {
		info, err := s.Info(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s（%s）· %s", info.Extra["server"], info.Version, info.Mode, info.Compat.Message), nil
	}, nil)

	// Detect disabled / renamed commands (common in production via rename-command).
	start := time.Now()
	var missing []string
	for _, c := range []string{"scan", "type", "ttl", "memory", "config", "info", "slowlog"} {
		res, err := s.cl.Do(ctx, "command", "info", c).Slice()
		if err != nil {
			missing = nil
			break
		}
		if len(res) == 0 || res[0] == nil {
			missing = append(missing, c)
		}
	}
	st := diag.Step{Name: "命令可用性", Status: diag.OK, Duration: time.Since(start).Milliseconds()}
	if len(missing) > 0 {
		st.Status = diag.Warn
		st.Detail = "以下命令被禁用或改名：" + strings.Join(missing, ", ")
		st.Hint = "相关功能将不可用（例如禁用 SCAN 时无法浏览 key）。"
	}
	rep.Add(st)
	return s, nil
}

func (s *Session) Close() { _ = s.cl.Close() }

func parseInfo(text string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			m[k] = v
		}
	}
	return m
}

func (s *Session) Info(ctx context.Context) (driver.ServerInfo, error) {
	info := driver.ServerInfo{Kind: "redis", Extra: map[string]string{}}
	var text string
	var err error
	if s.cluster != nil {
		// Any node is fine for version info.
		err = s.cluster.ForEachShard(ctx, func(ctx context.Context, c *goredis.Client) error {
			if text == "" {
				text, err = c.Info(ctx, "server").Result()
			}
			return err
		})
	} else {
		text, err = s.cl.Info(ctx, "server").Result()
	}
	if err != nil {
		return info, err
	}
	m := parseInfo(text)
	info.Version = m["redis_version"]
	info.Mode = m["redis_mode"]
	info.Extra["server"] = "Redis"
	switch {
	case m["server_name"] == "valkey" || m["valkey_version"] != "":
		info.Extra["server"] = "Valkey"
		if v := m["valkey_version"]; v != "" {
			info.Extra["serverVersion"] = v
		}
	case m["dragonfly_version"] != "":
		info.Extra["server"] = "Dragonfly"
		info.Extra["serverVersion"] = m["dragonfly_version"]
	case strings.Contains(strings.ToLower(text), "keydb"):
		info.Extra["server"] = "KeyDB"
	}
	info.Extra["os"] = m["os"]
	info.Extra["uptimeDays"] = m["uptime_in_days"]
	info.Compat = compat.Check("redis", info.Version)
	if info.Extra["server"] != "Redis" {
		info.Compat.Message += fmt.Sprintf("（检测到兼容实现 %s，部分命令行为可能不同）", info.Extra["server"])
	}
	return info, nil
}

// Overview returns INFO sections parsed into maps for dashboards.
func (s *Session) Overview(ctx context.Context) (map[string]string, error) {
	var text string
	var err error
	if s.cluster != nil {
		err = s.cluster.ForEachMaster(ctx, func(ctx context.Context, c *goredis.Client) error {
			if text == "" {
				text, err = c.Info(ctx).Result()
			}
			return err
		})
	} else {
		text, err = s.cl.Info(ctx).Result()
	}
	if err != nil {
		return nil, err
	}
	return parseInfo(text), nil
}

type KeyItem struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	TTL  int64  `json:"ttl"` // seconds, -1 no expiry
}

type ScanResult struct {
	Keys   []KeyItem `json:"keys"`
	Cursor uint64    `json:"cursor"` // 0 = done (standalone); cluster scans all masters at once
	Done   bool      `json:"done"`
}

// Scan iterates keys with SCAN (never KEYS).
func (s *Session) Scan(ctx context.Context, pattern string, cursor uint64, count int64) (ScanResult, error) {
	if pattern == "" {
		pattern = "*"
	}
	if count <= 0 {
		count = 500
	}
	var res ScanResult
	var keys []string
	if s.cluster != nil {
		var mu sync.Mutex
		err := s.cluster.ForEachMaster(ctx, func(ctx context.Context, c *goredis.Client) error {
			var cur uint64
			for n := 0; n < 20; n++ {
				ks, next, err := c.Scan(ctx, cur, pattern, count).Result()
				if err != nil {
					return err
				}
				mu.Lock()
				keys = append(keys, ks...)
				full := int64(len(keys)) >= count*4
				mu.Unlock()
				cur = next
				if cur == 0 || full {
					break
				}
			}
			return nil
		})
		if err != nil {
			return res, err
		}
		res.Done = true
	} else {
		ks, next, err := s.cl.Scan(ctx, cursor, pattern, count).Result()
		if err != nil {
			return res, err
		}
		keys, res.Cursor, res.Done = ks, next, next == 0
	}
	sort.Strings(keys)
	types := make([]*goredis.StatusCmd, len(keys))
	ttls := make([]*goredis.DurationCmd, len(keys))
	pipe := s.cl.Pipeline()
	for i, k := range keys {
		types[i] = pipe.Type(ctx, k)
		ttls[i] = pipe.TTL(ctx, k)
	}
	if len(keys) > 0 {
		_, _ = pipe.Exec(ctx)
	}
	for i, k := range keys {
		ttl := int64(-1)
		if d, err := ttls[i].Result(); err == nil && d > 0 {
			ttl = int64(d / time.Second)
		}
		res.Keys = append(res.Keys, KeyItem{Key: k, Type: types[i].Val(), TTL: ttl})
	}
	return res, nil
}

type Field struct {
	Field string  `json:"field,omitempty"`
	Value string  `json:"value"`
	Score float64 `json:"score,omitempty"`
	ID    string  `json:"id,omitempty"`
}

type KeyValue struct {
	Key    string  `json:"key"`
	Type   string  `json:"type"`
	TTL    int64   `json:"ttl"`
	Size   int64   `json:"size"` // length / cardinality
	Memory int64   `json:"memory"`
	String string  `json:"string,omitempty"`
	Items  []Field `json:"items,omitempty"`
	// Truncated is true when only the first page of a big collection is shown.
	Truncated bool `json:"truncated"`
}

const pageSize = 500

func (s *Session) GetKey(ctx context.Context, key string) (KeyValue, error) {
	v := KeyValue{Key: key}
	typ, err := s.cl.Type(ctx, key).Result()
	if err != nil {
		return v, err
	}
	if typ == "none" {
		return v, fmt.Errorf("key %q 不存在", key)
	}
	v.Type = typ
	v.TTL = -1
	if d, err := s.cl.TTL(ctx, key).Result(); err == nil && d > 0 {
		v.TTL = int64(d / time.Second)
	}
	if n, err := s.cl.MemoryUsage(ctx, key).Result(); err == nil {
		v.Memory = n
	}
	switch typ {
	case "string":
		v.String, err = s.cl.Get(ctx, key).Result()
		v.Size = int64(len(v.String))
	case "list":
		v.Size, _ = s.cl.LLen(ctx, key).Result()
		var xs []string
		xs, err = s.cl.LRange(ctx, key, 0, pageSize-1).Result()
		for i, x := range xs {
			v.Items = append(v.Items, Field{Field: strconv.Itoa(i), Value: x})
		}
	case "hash":
		v.Size, _ = s.cl.HLen(ctx, key).Result()
		var ks []string
		ks, _, err = s.cl.HScan(ctx, key, 0, "*", pageSize).Result()
		for i := 0; i+1 < len(ks); i += 2 {
			v.Items = append(v.Items, Field{Field: ks[i], Value: ks[i+1]})
		}
	case "set":
		v.Size, _ = s.cl.SCard(ctx, key).Result()
		var ks []string
		ks, _, err = s.cl.SScan(ctx, key, 0, "*", pageSize).Result()
		for _, x := range ks {
			v.Items = append(v.Items, Field{Value: x})
		}
	case "zset":
		v.Size, _ = s.cl.ZCard(ctx, key).Result()
		var zs []goredis.Z
		zs, err = s.cl.ZRangeWithScores(ctx, key, 0, pageSize-1).Result()
		for _, z := range zs {
			v.Items = append(v.Items, Field{Value: fmt.Sprint(z.Member), Score: z.Score})
		}
	case "stream":
		v.Size, _ = s.cl.XLen(ctx, key).Result()
		var xs []goredis.XMessage
		xs, err = s.cl.XRevRangeN(ctx, key, "+", "-", pageSize).Result()
		for _, x := range xs {
			var parts []string
			for k, val := range x.Values {
				parts = append(parts, fmt.Sprintf("%s=%v", k, val))
			}
			sort.Strings(parts)
			v.Items = append(v.Items, Field{ID: x.ID, Value: strings.Join(parts, " ")})
		}
	default:
		v.String = "(暂不支持查看该类型：" + typ + ")"
	}
	v.Truncated = int64(len(v.Items)) < v.Size && typ != "string"
	return v, err
}

func (s *Session) SetString(ctx context.Context, key, value string, ttlSeconds int64) error {
	return s.cl.Set(ctx, key, value, time.Duration(ttlSeconds)*time.Second).Err()
}

func (s *Session) SetTTL(ctx context.Context, key string, ttlSeconds int64) error {
	if ttlSeconds < 0 {
		return s.cl.Persist(ctx, key).Err()
	}
	return s.cl.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second).Err()
}

func (s *Session) DeleteKeys(ctx context.Context, keys []string) (int64, error) {
	var n int64
	for _, k := range keys { // one by one so it also works across cluster slots
		c, err := s.cl.Unlink(ctx, k).Result()
		if err != nil {
			return n, err
		}
		n += c
	}
	return n, nil
}

// Exec runs a raw console command and returns a printable result.
func (s *Session) Exec(ctx context.Context, line string) (string, error) {
	args, err := splitArgs(line)
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		return "", nil
	}
	if strings.EqualFold(args[0].(string), "monitor") || strings.EqualFold(args[0].(string), "subscribe") {
		return "", errors.New("控制台不支持阻塞命令 " + args[0].(string))
	}
	res, err := s.cl.Do(ctx, args...).Result()
	if errors.Is(err, goredis.Nil) {
		return "(nil)", nil
	}
	if err != nil {
		return "", err
	}
	return format(res, ""), nil
}

func format(v any, indent string) string {
	switch x := v.(type) {
	case nil:
		return "(nil)"
	case string:
		return strconv.Quote(x)
	case int64:
		return "(integer) " + strconv.FormatInt(x, 10)
	case []any:
		if len(x) == 0 {
			return "(empty array)"
		}
		var b strings.Builder
		for i, e := range x {
			if i > 0 {
				b.WriteString("\n" + indent)
			}
			fmt.Fprintf(&b, "%d) %s", i+1, format(e, indent+"   "))
		}
		return b.String()
	case map[any]any:
		var b strings.Builder
		i := 0
		for k, e := range x {
			if i > 0 {
				b.WriteString("\n" + indent)
			}
			fmt.Fprintf(&b, "%v => %s", k, format(e, indent+"   "))
			i++
		}
		return b.String()
	}
	return fmt.Sprint(v)
}

// splitArgs splits a console line honoring double/single quotes.
func splitArgs(line string) ([]any, error) {
	var args []any
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("引号未闭合")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

type SlowLog struct {
	ID       int64  `json:"id"`
	Time     int64  `json:"time"`
	Duration int64  `json:"durationUs"`
	Command  string `json:"command"`
	Client   string `json:"client"`
}

func (s *Session) SlowLog(ctx context.Context, n int64) ([]SlowLog, error) {
	logs, err := s.cl.SlowLogGet(ctx, n).Result()
	if err != nil {
		return nil, err
	}
	out := make([]SlowLog, 0, len(logs))
	for _, l := range logs {
		out = append(out, SlowLog{ID: l.ID, Time: l.Time.Unix(), Duration: l.Duration.Microseconds(), Command: strings.Join(l.Args, " "), Client: l.ClientAddr})
	}
	return out, nil
}
