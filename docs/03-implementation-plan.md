# x-monitor 实施计划与验收标准

## 1. 开发目录

目标目录：

```text
/Users/daniel/project/code/agent/x-monitor
```

该目录是独立 Go 项目和独立 Git 仓库，不依赖相邻的 `codex-grok-search` 工作目录。

## 2. 预定项目结构

```text
x-monitor/
├── cmd/
│   └── x-monitor/
│       └── main.go
├── internal/
│   ├── app/
│   ├── domain/
│   ├── ports/
│   ├── application/
│   │   ├── monitor/
│   │   └── delivery/
│   ├── adapters/
│   │   ├── grok/
│   │   ├── sqlite/
│   │   ├── telegram/
│   │   └── filelock/
│   ├── infrastructure/
│   │   ├── config/
│   │   ├── logging/
│   │   ├── scheduler/
│   │   └── buildinfo/
│   └── commands/
├── configs/
├── deploy/
│   └── systemd/
├── docs/
├── testdata/
├── sqlc.yaml
├── .golangci.yml
├── .goreleaser.yaml
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── README.zh-CN.md
```

## 3. 阶段一：项目骨架

交付：

- 独立 Git 仓库。
- Go module。
- Cobra 命令骨架。
- Uber Fx 模块骨架。
- Koanf TOML 配置。
- Validator 配置校验。
- Zap JSON 日志。
- 构建信息。

命令：

```text
x-monitor serve
x-monitor run
x-monitor status
x-monitor doctor
x-monitor migrate
x-monitor version
```

验收：

- 配置合法时应用可以启动并优雅关闭。
- 缺少必要配置时返回明确错误。
- Token 不出现在日志。
- `go test ./...` 通过。

## 4. 阶段二：SQLite

交付：

- modernc SQLite。
- WAL 和 foreign keys。
- Goose 嵌入迁移。
- sqlc schema 和 queries。
- accounts、runs、posts、deliveries、monitor_state。
- repository adapters。
- transaction manager。

验收：

- 新数据库自动迁移。
- 重复 status URL 无法插入第二次。
- 同一 status URL 和 destination 只有一个 delivery。
- 数据库重启后状态不丢失。
- SQLite 文件权限受限。

## 5. 阶段三：Grok Adapter

交付：

- Grok CLI 查找和绝对路径配置。
- 私有 run directory。
- 临时 HOME/GROK_HOME/TMPDIR。
- 认证 JSON 安全复制和刷新写回。
- 环境变量白名单。
- 固定工具和模型参数。
- 超时和进程组终止。
- stdout/stderr/manifest 保留。
- 结构化结果解析。

验收：

- Grok 不能看到当前源码仓库。
- Telegram Token 不进入 Grok 环境。
- 超时时完整终止子进程。
- 非法 JSON 返回结构化错误。
- 部分输出能保留但不推进成功游标。
- Fake Grok CLI 集成测试通过。

## 6. 阶段四：监控业务

交付：

- 五个账户批量搜索。
- 原创、回复、转帖。
- 两小时正常窗口。
- 首次只报告一小时。
- 24 小时最大恢复窗口。
- URL 规范化。
- URL 去重。
- 北京时间转换。
- 不确定字段处理。

验收：

- 同一 URL 被 Grok 返回多次时只保存一次。
- twitter.com 和带 query 的 URL 规范化为 x.com。
- 非监控账户结果被拒绝。
- 超出时间范围的结果不会作为新帖发送。
- 无法确认类型时显示“不确定”，不猜测。

## 7. 阶段五：Telegram 和 Outbox

交付：

- go-telegram/bot adapter。
- 一帖一消息。
- 按发布时间从旧到新。
- pending/processing/retry/sent/dead 状态机。
- 发送 claim lease。
- backoff 重试。
- Telegram 429 RetryAfter。

验收：

- 没有新帖时不发送消息。
- 发送成功记录 Telegram message ID。
- 401/403 进入永久错误。
- 429 按 RetryAfter 重试。
- 5xx 和网络错误使用指数退避。
- 服务重启后 pending delivery 继续发送。

## 8. 阶段六：gocron

交付：

- `0 * * * *` 整点调度。
- `Asia/Shanghai` 时区。
- singleton reschedule。
- 启动补跑。
- 50 分钟任务超时。
- SIGTERM 优雅关闭。
- gofrs/flock 跨进程锁。

验收：

- 同一进程任务不会重叠。
- 两个进程不会同时执行监控。
- 上一轮仍运行时下一轮跳过并记录结构化日志。
- 不依赖 Linux cron 或 systemd timer。

## 9. 阶段七：质量与发布

交付：

- 单元测试。
- SQLite 集成测试。
- Fake Grok 测试。
- Telegram HTTP 测试。
- goroutine 泄漏测试。
- golangci-lint。
- govulncheck。
- GoReleaser。
- Linux systemd service。
- 中英文部署文档。

持续检查：

```text
go test ./...
go test -race ./...
go vet ./...
golangci-lint run
govulncheck ./...
go mod verify
goose validate
sqlc generate
```

## 10. 业务验收标准

必须满足：

1. 每小时监控：
   - `@CryptoDinduz`
   - `@2442lll`
   - `@iruletrenches`
   - `@btc_eth_owner`
   - `@yeonwoo1102`
   - `@xtony1314`
   - `@Crypto_Cat888`
   - `@AIonBase_`
   - `@blu0222`
   - `@ShawnThread`
2. 覆盖原创、回复和转帖。
3. 不设置关键词过滤。
4. 正常查询最近两小时。
5. 首次只报告此前一小时。
6. X direct status URL 是唯一标识。
7. 相同 URL 只报告一次。
8. 无新帖不发送 Telegram。
9. 每条消息包含：
   - 账户
   - 内容类型
   - Asia/Shanghai 发布时间
   - 原帖直链
   - 准确简洁的中文摘要
10. 按发布时间从旧到新发送。
11. 字段不确定时明确标注。
12. Grok 失败时不推进成功游标。
13. Telegram 失败时保留待发送状态。
14. 上一轮仍运行时跳过本轮。
15. 重启后去重状态和投递状态不丢失。

## 11. 已知限制

- Grok X Search 是搜索系统，不是 X 事件流，不能保证官方 API 等级的完整召回。
- 纯 repost 可能无法通过公开搜索完整返回。
- Telegram 没有通用幂等键，极窄的崩溃窗口可能造成一次重复发送。
- 最大恢复窗口以外的停机时间可能导致遗漏。
- 第一版为单机部署，不设计多节点高可用。

## 12. 开发启动条件

开始实现不需要真实 Telegram Token，可以使用测试服务器和环境变量占位。

真实联调阶段需要：

- 正式安装的 Grok CLI。
- 已完成的 Grok 登录。
- Telegram Bot Token。
- Telegram chat ID。
