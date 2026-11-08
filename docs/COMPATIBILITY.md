# 版本支持 / Compatibility

MidDeck 连接后自动识别服务端版本，并对照下表给出提示。表中的规则定义在 [`internal/compat/compat.go`](../internal/compat/compat.go)，两处需保持一致。

| 等级 | 含义 |
|---|---|
| 已实测 | 该版本线在每次 CI 中跑集成测试（见 `.github/workflows/build.yml` 的 integration 任务） |
| 支持 | 预期所有功能可用 |
| 部分支持 | 可以连接，部分功能不可用（界面会列出） |
| 不支持 | 版本过旧，建议只做只读查看 |

## Kafka

| 已实测 | 完整支持 | 部分支持 |
|---|---|---|
| 3.9、4.0 | ≥ 2.1 | ≥ 0.11 |

版本相关功能：

| 功能 | 最低版本 |
|---|---|
| 增量修改 Topic 配置 | 2.3 |
| 删除消费组 offset | 2.4 |
| DescribeCluster | 2.8 |
| KRaft 模式 | 3.3 |

Kafka 没有直接返回版本号的接口，MidDeck 根据 ApiVersions 响应推断版本。阿里云、MSK、Confluent Cloud 等托管服务，以及 Redpanda 等兼容实现，可能被识别为某个"兼容版本"。

## Redis

| 已实测 | 完整支持 | 部分支持 |
|---|---|---|
| 6.2、7.4 | ≥ 5.0 | ≥ 3.0 |

| 功能 | 最低版本 |
|---|---|
| Stream | 5.0 |
| ACL、RESP3 | 6.0 |
| Functions | 7.0 |
| Hash 字段 TTL | 7.4 |

还会识别 Valkey、Dragonfly、KeyDB，并检测被 `rename-command` 禁用的命令。云厂商的代理模式可能限制部分命令。

## RabbitMQ

| 已实测 | 完整支持 | 部分支持 |
|---|---|---|
| 3.13、4.1 | ≥ 3.8 | ≥ 3.6 |

| 功能 | 版本要求 |
|---|---|
| Quorum 队列 | ≥ 3.8 |
| Stream 队列 | ≥ 3.9 |
| 经典镜像队列 | 4.0 起移除 |

需要启用 `rabbitmq_management` 插件。

## 新增实测版本

1. 在 `.github/workflows/build.yml` 的 `integration` 矩阵中加一组镜像版本；
2. 在 `internal/compat/compat.go` 的 `Tested` 中加上该版本线，并同步本文档。

集成测试会断言：CI 中跑的版本必须出现在 `Tested` 列表里。
