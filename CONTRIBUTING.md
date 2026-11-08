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

想在本地起几个中间件做测试，可以用 Docker：

```bash
docker run -d --name kafka -p 9092:9092 apache/kafka:3.9.0
docker run -d --name redis -p 6379:6379 redis:7.4
docker run -d --name rabbit -p 5672:5672 -p 15672:15672 rabbitmq:4-management
```

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
