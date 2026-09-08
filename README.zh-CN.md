# x-monitor

`x-monitor` 是一个独立的 Go 常驻服务：通过本机已登录的 Grok CLI 调用
X Search，监控指定公开账户，并把尚未发送的新帖子推送给 Telegram Bot。
运行时不需要 Codex，也不在本地运行大模型。

## 当前监控规则

- 账户：`@CryptoDinduz`、`@2442lll`、`@iruletrenches`、
  `@btc_eth_owner`、`@yeonwoo1102`、`@xtony1314`、`@Crypto_Cat888`、
  `@AIonBase_`、`@blu0222`
- 内容：原创、回复、转帖
- 关键词：不限制
- 调度：每小时整点，`Asia/Shanghai`
- 正常检索窗口：最近 2 小时
- 首次运行：检索 2 小时，但只通知最近 1 小时
- 唯一标识：规范化后的 `https://x.com/<handle>/status/<id>`
- 没有新帖：不调用 Telegram

## 系统组成

```text
Cobra CLI
  └─ Uber Fx 生命周期
      ├─ Koanf 配置 + Validator 校验
      ├─ gocron 进程内调度
      ├─ Grok CLI x_search 适配器
      ├─ SQLite + Goose + sqlc
      ├─ Telegram Bot SDK
      ├─ Outbox + backoff 重试
      └─ gofrs/flock 跨进程锁
```

Grok CLI 使用临时 `HOME`、`GROK_HOME`、`TMPDIR` 和环境变量白名单。服务只
向子进程开放 `x_search`，不把 Telegram Token、源码目录或完整宿主环境传入
Grok。每轮原始结果、stderr 和 manifest 保存在私有 run 目录，便于审计。

## 开发环境

要求：

- Go 1.26.x
- Grok CLI（真实联调时需要）
- sqlc 1.31.1（修改 SQL 后重新生成代码时需要）

```bash
go mod download
go test ./...
go test -race ./...
go vet ./...
mkdir -p bin
go build -o bin/x-monitor ./cmd/x-monitor
./bin/x-monitor version
```

修改 `internal/adapters/sqlite/queries/` 或迁移后执行：

```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
```

## 配置

复制示例，但不要把真实 Telegram Token 写进 TOML：

```bash
cp configs/config.example.toml configs/config.toml
chmod 600 configs/config.toml
export XMONITOR_TELEGRAM_TOKEN='真实 Bot Token'
```

然后修改：

- `[grok].binary`：Grok CLI 的固定绝对路径
- `[telegram].chat_id`：目标私聊、群组或频道 chat ID
- `[storage]`：数据库、运行产物和锁文件路径

环境变量可以覆盖 TOML。前缀是 `XMONITOR_CFG_`，层级用双下划线分隔：

```bash
export XMONITOR_CFG_LOGGING__LEVEL=debug
export XMONITOR_CFG_SCHEDULER__CRON='15 * * * *'
```

Telegram Token 使用 `[telegram].token_env` 指定的独立环境变量读取；默认是
`XMONITOR_TELEGRAM_TOKEN`。

如果服务必须通过本机代理访问 xAI 和 Telegram，在受限的 EnvironmentFile
中配置标准代理变量。Grok 隔离器只会把这些代理变量和少量基础环境变量传给
Grok CLI，不会传递 Telegram Token：

```bash
HTTP_PROXY=http://127.0.0.1:7891
HTTPS_PROXY=http://127.0.0.1:7891
ALL_PROXY=socks5h://127.0.0.1:7891
NO_PROXY=127.0.0.1,localhost,::1
```

## 命令

```bash
# 检查配置、数据库、Grok 登录和 Telegram 凭据
x-monitor --config configs/config.toml doctor

# 自动创建/升级 SQLite
x-monitor --config configs/config.toml migrate

# 立即运行一轮
x-monitor --config configs/config.toml run

# 启动进程内 gocron 调度器
x-monitor --config configs/config.toml serve

# 查看上次运行和 Outbox 状态
x-monitor --config configs/config.toml status

x-monitor version
```

推荐首次部署顺序：

1. 使用服务运行用户执行 Grok CLI 登录。
2. 执行 `doctor`，确认 Grok 和 Telegram 都返回 `ok`。
3. 执行一次 `run`，观察 SQLite、run artifacts 和 Telegram。
4. 再启动 `serve`。

## Linux systemd 部署

systemd 只负责守护进程；定时逻辑仍由 Go 进程中的 gocron 完成，不使用
systemd timer。

```bash
sudo useradd --system --home /var/lib/x-monitor --create-home x-monitor
sudo install -d -o x-monitor -g x-monitor -m 0700 /var/lib/x-monitor/runs
sudo install -d -o root -g x-monitor -m 0750 /etc/x-monitor
sudo install -o root -g root -m 0755 bin/x-monitor /usr/local/bin/x-monitor
sudo install -o root -g x-monitor -m 0640 \
  configs/config.toml /etc/x-monitor/config.toml
sudo install -o root -g x-monitor -m 0640 \
  deploy/systemd/x-monitor.env.example /etc/x-monitor/x-monitor.env
sudo install -o root -g root -m 0644 \
  deploy/systemd/x-monitor.service /etc/systemd/system/x-monitor.service
```

把真实 Token 写入 `/etc/x-monitor/x-monitor.env`，并以 `x-monitor` 用户完成
Grok 登录。确认 `/var/lib/x-monitor/.grok/auth.json` 只对服务用户可读。

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now x-monitor
sudo systemctl status x-monitor
journalctl -u x-monitor -f
```

无 sudo 权限但已经启用 systemd user manager 和 linger 时，可以改用
`deploy/systemd/x-monitor-user.service`，将二进制、配置和数据分别安装到
`~/.local/bin`、`~/.config/x-monitor` 和 `~/.local/share/x-monitor`。该 unit
只开放数据目录和 `~/.grok` 的写权限，以便 Grok 安全刷新认证文件。

## 数据与恢复

- SQLite 默认使用 WAL、foreign keys 和 busy timeout。
- `posts.status_url` 是主键；同一帖子不会生成第二条投递。
- 帖子与 pending delivery 在同一事务内写入。
- Telegram 发送使用 processing lease；进程中断后会恢复为 retry。
- Grok 失败不会产生成功游标；下轮会按恢复窗口回查。
- 最大恢复窗口默认 24 小时，超过该范围的停机可能造成遗漏。

Telegram Bot API 没有通用幂等键。在“Telegram 已接收消息、进程尚未把
message ID 写回 SQLite”这一极窄崩溃窗口内，理论上可能重复发送一次。

## 安全边界

- 配置文件和 run artifacts 建议分别使用 `0600`、目录使用 `0700`。
- 生产环境给 Grok CLI 配置固定绝对路径，不依赖可变 `PATH`。
- Token 只从环境变量/受限 EnvironmentFile 注入。
- 不要把数据库、run artifacts、真实配置和 `.grok/auth.json` 提交到 Git。
- Grok 输出是不可信外部数据，只解析为受限 JSON，不作为命令执行。

完整设计见：

- [系统架构](docs/01-system-architecture.md)
- [技术选型](docs/02-technology-selection.md)
- [实施与验收](docs/03-implementation-plan.md)
- [设计决策与目录命名](docs/04-design-decisions.md)
- [Linux 部署指南](docs/05-deployment-guide.md)
