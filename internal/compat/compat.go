// Package compat holds MidDeck's version-support matrix and evaluates a
// detected server version against it. Keep docs/COMPATIBILITY.md in sync.
package compat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Level string

const (
	Tested      Level = "tested"      // covered by CI integration tests
	Supported   Level = "supported"   // expected to work fully
	Partial     Level = "partial"     // connects, some features unavailable
	Unsupported Level = "unsupported" // too old; read-only at your own risk
	Unknown     Level = "unknown"     // version could not be detected
)

// Feature is a capability gated by a minimum server version.
type Feature struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Min   string `json:"min"`
}

// Matrix is the support policy for one product.
type Matrix struct {
	Product      string    `json:"product"`
	Tested       []string  `json:"tested"`       // major.minor lines exercised by the integration job in .github/workflows/build.yml
	SupportedMin string    `json:"supportedMin"` // >= fully supported
	PartialMin   string    `json:"partialMin"`   // >= partially supported
	Features     []Feature `json:"features"`
}

var Matrices = map[string]Matrix{
	"kafka": {
		Product: "Apache Kafka", Tested: []string{"3.9", "4.0"},
		SupportedMin: "2.1", PartialMin: "0.11",
		Features: []Feature{
			{"incrementalAlterConfigs", "增量修改 Topic 配置", "2.3"},
			{"offsetDelete", "删除消费组 offset", "2.4"},
			{"describeCluster", "DescribeCluster / KRaft 信息", "2.8"},
			{"kraft", "KRaft 模式（无 ZooKeeper）", "3.3"},
		},
	},
	"redis": {
		Product: "Redis", Tested: []string{"6.2", "7.4"},
		SupportedMin: "5.0", PartialMin: "3.0",
		Features: []Feature{
			{"stream", "Stream 数据类型", "5.0"},
			{"acl", "ACL 用户管理", "6.0"},
			{"resp3", "RESP3 协议", "6.0"},
			{"functions", "Functions", "7.0"},
			{"hashFieldTTL", "Hash 字段 TTL", "7.4"},
		},
	},
	"rabbitmq": {
		Product: "RabbitMQ", Tested: []string{"3.13", "4.1"},
		SupportedMin: "3.8", PartialMin: "3.6",
		Features: []Feature{
			{"quorumQueue", "Quorum 队列", "3.8"},
			{"stream", "Stream 队列", "3.9"},
			{"mirroredQueue", "经典镜像队列（4.0 起移除）", "0"},
		},
	},
}

// Result is what the UI shows next to a connected instance.
type Result struct {
	Product     string   `json:"product"`
	Version     string   `json:"version"`
	Level       Level    `json:"level"`
	Message     string   `json:"message"`
	Unavailable []string `json:"unavailable"` // feature labels not available on this version
}

// Check evaluates version against the matrix for kind.
func Check(kind, version string) Result {
	m, ok := Matrices[kind]
	if !ok {
		return Result{Version: version, Level: Unknown, Message: "未知中间件类型"}
	}
	r := Result{Product: m.Product, Version: version}
	if version == "" {
		r.Level = Unknown
		r.Message = "无法识别服务端版本，部分功能可能不可用"
		return r
	}
	for _, f := range m.Features {
		if f.Min != "0" && Compare(version, f.Min) < 0 {
			r.Unavailable = append(r.Unavailable, fmt.Sprintf("%s（需要 %s+）", f.Label, f.Min))
		}
	}
	mm := majorMinor(version)
	switch {
	case contains(m.Tested, mm):
		r.Level = Tested
		r.Message = fmt.Sprintf("%s %s 已通过 MidDeck 集成测试", m.Product, version)
	case Compare(version, m.SupportedMin) >= 0:
		r.Level = Supported
		r.Message = fmt.Sprintf("%s %s 在支持范围内（>= %s）", m.Product, version, m.SupportedMin)
	case Compare(version, m.PartialMin) >= 0:
		r.Level = Partial
		r.Message = fmt.Sprintf("%s %s 低于完整支持版本 %s，部分功能不可用", m.Product, version, m.SupportedMin)
	default:
		r.Level = Unsupported
		r.Message = fmt.Sprintf("%s %s 低于最低支持版本 %s，建议仅做只读查看", m.Product, version, m.PartialMin)
	}
	if kind == "rabbitmq" && Compare(version, "4.0") >= 0 {
		r.Unavailable = append(r.Unavailable, "经典镜像队列（4.0 起移除）")
	}
	return r
}

var numRe = regexp.MustCompile(`\d+`)

func parts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// stop at first non version char (e.g. "3.9.0-IV1", "7.2.4 (valkey)")
	if i := strings.IndexAny(v, " -+("); i > 0 {
		v = v[:i]
	}
	var out []int
	for _, s := range numRe.FindAllString(v, 3) {
		n, _ := strconv.Atoi(s)
		out = append(out, n)
	}
	return out
}

// Compare compares dotted versions: -1, 0, 1.
func Compare(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func majorMinor(v string) string {
	p := parts(v)
	if len(p) >= 2 {
		return fmt.Sprintf("%d.%d", p[0], p[1])
	}
	return v
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
