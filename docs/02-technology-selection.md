# x-monitor 技术选型

## 1. 选型原则

成熟、维护活跃并能显著减少样板代码的第三方模块优先。

以下底层能力继续使用 Go 标准库：

- `context`
- `os/exec`
- `net/url`
- `encoding/json`
- `crypto/*`
- `time`
- `errors`
- `io/fs`
- `embed`

原因是这些属于语言和运行时的安全基础。仅仅再套一层第三方薄包装不会增加能力，反而扩大供应链攻击面。

应用层不直接使用：

- 原始 `flag`
- 裸 `database/sql` 查询
- 裸 Telegram HTTP
- 手写 Cron
- 手写迁移器
- 手写配置合并
- 手写指数退避
- 手写跨平台文件锁

## 2. 运行时技术栈

| 领域 | 选型 | 基准版本 | 用途 |
|---|---|---:|---|
| Go | Go | 1.26.x | 语言和工具链 |
| 生命周期/DI | `go.uber.org/fx` | v1.24.0 | 模块组装、启动和关闭 |
| CLI | `github.com/spf13/cobra` | v1.10.2 | 子命令、参数、帮助 |
| 配置 | `github.com/knadh/koanf/v2` | v2.3.4 | TOML、环境变量、配置合并 |
| 验证 | `github.com/go-playground/validator/v10` | v10.30.2 | 配置和结构化结果验证 |
| 调度 | `github.com/go-co-op/gocron/v2` | v2.22.0 | Cron、时区、singleton |
| 日志 | `go.uber.org/zap` | v1.28.0 | JSON 结构化日志 |
| SQLite | `modernc.org/sqlite` | v1.55.0 | 无 CGO SQLite 驱动 |
| 迁移 | `github.com/pressly/goose/v3` | v3.27.1 | 嵌入式 SQL 迁移 |
| Telegram | `github.com/go-telegram/bot` | v1.21.0 | Telegram Bot API SDK |
| 重试 | `github.com/cenkalti/backoff/v7` | v7.0.0 | 指数退避和永久错误 |
| 文件锁 | `github.com/gofrs/flock` | v0.13.0 | 非阻塞跨平台文件锁 |
| ID | `github.com/google/uuid` | v1.6.0 | run 和 owner ID |

所有版本在开发开始时复核兼容性，然后固定到 `go.mod` 和 `go.sum`。生产构建不使用浮动 `latest`。

## 3. 开发工具

| 工具 | 用途 |
|---|---|
| `sqlc` v1.31.1 | 从 SQL 生成类型安全 Go 查询代码 |
| `golangci-lint` | 统一静态检查 |
| `govulncheck` | Go 依赖漏洞检查 |
| `goreleaser` | 多平台构建和校验和 |
| `testify` | 测试断言和 mock |
| `goleak` | goroutine 泄漏检查 |

## 4. 不采用的方案

### 4.1 不采用 GORM

原因：

- Outbox 需要精确事务边界。
- Delivery claim 需要条件更新。
- SQLite 并发行为必须清楚可见。
- `INSERT ... ON CONFLICT` 是核心逻辑。
- ORM 容易隐藏实际 SQL 和锁行为。

采用 sqlc：SQL 明确、Go 调用类型安全。

### 4.2 不采用 Gin、Echo 或 Fiber

第一版没有对外 Web API。服务是常驻调度进程，不需要 Web 框架。

如果未来增加本地健康接口，优先通过 Fx 单独提供一个最小 HTTP module，而不是让业务依赖 Web 框架。

### 4.3 不采用 Viper

Koanf 的 provider、parser 和加载顺序更显式，避免隐式全局状态和难以追踪的键覆盖。

### 4.4 不采用 Linux 定时器

不使用：

- cron
- systemd timer

使用 gocron 在 Go 进程内部调度。systemd 只能作为进程守护器。

### 4.5 不采用微服务

单台服务器、五个账户、每小时一次，不需要 Redis、Kafka、PostgreSQL 或 Kubernetes。

## 5. Fx 模块划分

```go
fx.New(
	config.Module,
	logging.Module,
	database.Module,
	filelock.Module,
	grok.Module,
	telegram.Module,
	monitor.Module,
	scheduler.Module,
)
```

每个模块：

- 只公开构造函数和接口绑定。
- 不使用 package-level 可变全局变量。
- 启动和关闭注册到 `fx.Lifecycle`。
- 可以在测试中单独替换。

## 6. 配置技术方案

配置文件使用 TOML：

```toml
[scheduler]
cron = "0 * * * *"
timezone = "Asia/Shanghai"
run_on_startup = true
job_timeout = "50m"

[monitor]
accounts = [
  "@CryptoDinduz",
  "@2442lll",
  "@iruletrenches",
  "@btc_eth_owner",
  "@yeonwoo1102",
  "@xtony1314",
  "@Crypto_Cat888",
  "@AIonBase_",
  "@blu0222",
  "@ShawnThread",
  "@theunipcs",
  "@stitchdegen"
]
content_types = ["original", "reply", "repost"]
normal_window = "2h"
first_run_report_window = "1h"
recovery_max_window = "24h"

[grok]
binary = "/home/x-monitor/.grok/bin/grok"
model = "grok-4.5"
depth = "quick"
timeout = "10m"
max_turns = 40
retention_days = 7

[telegram]
chat_id = "123456789"
token_env = "XMONITOR_TELEGRAM_TOKEN"

[storage]
database = "/var/lib/x-monitor/state.db"
runs_directory = "/var/lib/x-monitor/runs"
lock_file = "/var/lib/x-monitor/x-monitor.lock"

[logging]
level = "info"
format = "json"
```

覆盖优先级：

```text
代码默认值
  < config.toml
  < XMONITOR_CFG_* 环境变量
```

Telegram Token 只允许来自环境变量或受限权限的 secrets 文件，不进入普通 TOML。

## 7. 日志和敏感信息

禁止记录：

- Telegram Token
- Grok auth JSON
- 完整环境变量
- API Key
- Cookie
- Git 凭据

日志统一包含：

- run ID
- trigger
- window start/end
- duration
- posts found/new
- deliveries sent/retried/dead
- error code

## 8. 依赖安全

开发阶段执行：

```text
go mod verify
govulncheck ./...
golangci-lint run
go test -race ./...
```

发布阶段：

- 固定工具链。
- 固定依赖版本。
- 生成 SHA-256 校验和。
- 记录构建 Git commit。
- 不在构建脚本中使用未固定的 `@latest`。
- 对 Grok CLI 使用固定绝对路径。
- 可选配置允许的 Grok 二进制 SHA-256。
