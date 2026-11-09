# 参与贡献

感谢你愿意改进 MidDeck！

## 开发环境

- Go 1.26+
- Node.js 20+
- Wails CLI：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Linux 额外需要：`libgtk-3-dev libwebkit2gtk-4.1-dev`，并以 `wails dev -tags webkit2_41` 运行

```bash
wails dev          # 启动开发模式
go test ./internal/...
cd frontend && npm run build   # 前端类型检查 + 构建
```

本地演练环境（Kafka、Redis、RabbitMQ，外加一台 SSH 跳板机，用来测试隧道）：

```bash
docker compose -f dev/docker-compose.yml up -d
./dev/seed.sh                 # 写入示例 topic、消息、消费组、key、队列
go run ./dev/seedconfig       # 把对应的连接配置加进 MidDeck
```

跳板机是 `ops / middeck @ localhost:2222`。通过它访问的 `kafka:19092`、`redis:6379` 只在 Docker 网络内可解析，模拟真实内网。

## 国际化

界面默认英文，支持中文。前端文案统一写成 `t('English text')`，中文翻译放在 `frontend/src/zh.ts`；后端文案（诊断步骤、提示、兼容性说明）在 `internal/i18n/i18n.go`。提交前运行 `npm run i18n:check`（CI 也会检查），确保没有漏翻的文案。

## AI 助手与 MCP

- AI 助手的动作目录在 `internal/ai/actions.go`，前端在 `frontend/src/Assistant.tsx` 展示，各视图通过 `intent` 属性接收并**只预填表单**。新增动作时两边一起改，绝不在收到动作后直接执行写操作。
- MCP 工具在 `mcp.go`，协议实现在 `internal/mcp`。只暴露只读能力；本地调试：`go build -tags desktop,production -o /tmp/middeck . && /tmp/middeck mcp`（macOS 需 `CGO_LDFLAGS="-framework UniformTypeIdentifiers"`）。

## 新增一种中间件

1. 在 `internal/driver/<name>/` 下实现 `Open(ctx, inst, dial, report)`。所有网络连接必须使用传入的 `dial`，这样 SSH 跳板和代理才能生效。
2. 实现 `driver.Session` 接口（`Info` 返回版本和兼容性，`Close`）。
3. 在 `internal/compat/compat.go` 和 `docs/COMPATIBILITY.md` 中添加支持矩阵。
4. 在 `app.go` 的 `open()` 中注册，在 `app_ops.go` 中暴露操作方法。
5. 在 `frontend/src/<name>/` 下添加界面。

## 约定

- 写操作必须经过 `App.write()`，以便遵守只读模式并记录审计日志。
- 破坏性操作在前端使用 `useDangerConfirm`。
- 涉及服务端版本差异的功能，在兼容矩阵里声明最低版本。
- 提交信息建议使用 [Conventional Commits](https://www.conventionalcommits.org/)，例如 `feat(kafka): ...`。

## 发布

维护者推送 `v*` 标签即可，GitHub Actions 会构建全部平台并创建 Release：

```bash
git tag v0.1.0
git push origin v0.1.0
```
