# x-monitor Linux 部署指南

本文给出两种部署方式：有 root 权限时使用系统级 systemd；普通用户且启用了
systemd user manager 时使用用户级 systemd。当前 `ssh jmk` 环境使用第二种。

文中的 Token、chat ID 和账户信息必须替换为自己的值。不要把真实 Token、
`auth.json`、生产配置或 SQLite 数据提交到 Git。

## 1. 运行时组成

```text
systemd user service
        │
        ▼
 x-monitor Go 进程
   ├─ gocron 每小时触发
   ├─ SQLite 保存状态和 Outbox
   ├─ Grok CLI ──► 127.0.0.1:7891 ──► xAI / X Search
   └─ Telegram ──► 127.0.0.1:7891 ──► Telegram Bot API
```

服务器不需要 Codex，也不需要 Python。它需要：

- Linux x86-64；
- 已认证的官方 Grok CLI；
- Telegram Bot Token 和目标 chat ID；
- 能访问 xAI 与 Telegram 的网络；
- 本方案中监听 `127.0.0.1:7891` 的 HTTP/SOCKS mixed proxy；
- 用户级部署时，正常工作的 `systemctl --user` 和 linger。

## 2. 本地构建 Linux 二进制

在项目根目录执行：

```bash
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -o bin/x-monitor-linux-amd64 ./cmd/x-monitor
shasum -a 256 bin/x-monitor-linux-amd64
```

`modernc.org/sqlite` 不依赖 CGO，因此可以在 macOS 上直接交叉编译 Linux 二进制。
发布时应记录 SHA-256、Git commit 和构建时间。

## 3. 用户级 systemd 部署（当前 jmk 方案）

### 3.1 检查运行条件

```bash
ssh jmk 'uname -srm'
ssh jmk 'systemctl --user is-system-running'
ssh jmk 'loginctl show-user "$USER" -p Linger'
ssh jmk 'ss -lnt | grep "127.0.0.1:7891"'
```

预期为 Linux x86-64、user manager 可用、`Linger=yes`，并且代理监听
`127.0.0.1:7891`。没有 linger 时需要管理员执行：

```bash
sudo loginctl enable-linger daniel
```

### 3.2 创建私有目录

```bash
ssh jmk 'install -d -m 0700 \
  "$HOME/.config/x-monitor" \
  "$HOME/.local/share/x-monitor" \
  "$HOME/.local/share/x-monitor/runs" \
  "$HOME/.config/systemd/user"; \
  install -d -m 0755 "$HOME/.local/bin"'
```

### 3.3 安装二进制和 unit

```bash
scp bin/x-monitor-linux-amd64 jmk:/tmp/x-monitor
scp deploy/systemd/x-monitor-user.service jmk:/tmp/x-monitor-user.service

ssh jmk 'install -m 0755 /tmp/x-monitor "$HOME/.local/bin/x-monitor"; \
  install -m 0644 /tmp/x-monitor-user.service \
    "$HOME/.config/systemd/user/x-monitor.service"'
```

安装后比较本地和远端 SHA-256，避免传输或替换错误：

```bash
shasum -a 256 bin/x-monitor-linux-amd64
ssh jmk 'sha256sum "$HOME/.local/bin/x-monitor"; \
  "$HOME/.local/bin/x-monitor" version'
```

### 3.4 安装并认证 Grok CLI

Grok CLI 必须以运行服务的同一个 Linux 用户安装和登录。使用 xAI 官方发布渠道，
不要安装来源不明的同名包。安装后至少记录版本、绝对路径和 SHA-256：

```bash
ssh jmk '"$HOME/.grok/bin/grok" --version; \
  sha256sum "$HOME/.grok/bin/grok"; \
  stat -c "%a %U:%G %n" "$HOME/.grok/auth.json"'
```

`auth.json` 应为服务用户所有，权限为 `0600`。当前 jmk 首次部署核验到的版本是
`0.2.117`，当时二进制 SHA-256 为：

```text
2f6fb370a798e7d6e04595e117a983969de333f65bfafbd812ee287c7fb2b83f
```

这是部署记录，不是永久信任值。升级 Grok 后版本和校验和必然变化，应重新核验官方
来源并更新运维记录，不能因为与旧值不同就直接判定为恶意。

### 3.5 写入服务配置

从示例复制一份配置，并改成用户目录：

```bash
scp configs/config.example.toml jmk:/tmp/x-monitor-config.toml
ssh jmk 'install -m 0600 /tmp/x-monitor-config.toml \
  "$HOME/.config/x-monitor/config.toml"'
```

`~/.config/x-monitor/config.toml` 至少需要使用以下路径：

```toml
[grok]
binary = "/home/daniel/.grok/bin/grok"

[storage]
database = "/home/daniel/.local/share/x-monitor/state.db"
runs_directory = "/home/daniel/.local/share/x-monitor/runs"
lock_file = "/home/daniel/.local/share/x-monitor/x-monitor.lock"
```

其余监控规则以 `configs/config.example.toml` 为准。服务器用户名不是 `daniel` 时，
必须替换这些绝对路径。

### 3.6 Telegram Token 与 chat ID

先向 Bot 发送 `/start` 或任意消息，再通过 Telegram Bot API 的 `getUpdates` 查看
返回 JSON 中的 `message.chat.id`。群组和频道 ID 通常为负数。程序不会猜测目标，
必须提供明确的 chat ID。

将秘密和环境覆盖写入 `~/.config/x-monitor/x-monitor.env`：

```bash
XMONITOR_TELEGRAM_TOKEN=替换为真实Token
XMONITOR_CFG_TELEGRAM__CHAT_ID=替换为真实ChatID

HTTP_PROXY=http://127.0.0.1:7891
HTTPS_PROXY=http://127.0.0.1:7891
ALL_PROXY=socks5h://127.0.0.1:7891
NO_PROXY=127.0.0.1,localhost,::1
```

然后限制权限：

```bash
ssh jmk 'chmod 0600 "$HOME/.config/x-monitor/x-monitor.env" \
  "$HOME/.config/x-monitor/config.toml"'
```

不要在日志、聊天、Git diff 或截图中展示该文件内容。

### 3.7 部署前验证

先在服务相同的环境变量下执行诊断：

```bash
ssh jmk 'set -a; . "$HOME/.config/x-monitor/x-monitor.env"; set +a; \
  "$HOME/.local/bin/x-monitor" \
    --config "$HOME/.config/x-monitor/config.toml" doctor'
```

预期输出：

```text
config ok
database ok
grok ok
telegram ok
```

然后手工执行一次真实监控：

```bash
ssh jmk 'set -a; . "$HOME/.config/x-monitor/x-monitor.env"; set +a; \
  "$HOME/.local/bin/x-monitor" \
    --config "$HOME/.config/x-monitor/config.toml" run'
```

首次运行只通知最近一小时内容。再次运行应因 status URL 去重而不重复发送。

### 3.8 启动服务

```bash
ssh jmk 'systemctl --user daemon-reload; \
  systemctl --user enable --now x-monitor.service; \
  systemctl --user --no-pager --full status x-monitor.service'
```

查看日志和业务状态：

```bash
ssh jmk 'journalctl --user -u x-monitor.service -n 100 --no-pager'
ssh jmk 'set -a; . "$HOME/.config/x-monitor/x-monitor.env"; set +a; \
  "$HOME/.local/bin/x-monitor" \
    --config "$HOME/.config/x-monitor/config.toml" status'
```

## 4. 验证所有外部流量经过 7891

配置代理变量只能证明“意图”，不能证明实际连接。应同时检查进程环境和 TCP 连接。

### 4.1 检查服务进程环境

```bash
ssh jmk 'pid=$(systemctl --user show x-monitor.service \
  --property MainPID --value); \
  tr "\0" "\n" < "/proc/$pid/environ" | \
  grep -E "^(HTTP_PROXY|HTTPS_PROXY|ALL_PROXY|NO_PROXY)="'
```

该命令只显示代理变量，不能打印整个环境，否则可能泄露 Telegram Token。

### 4.2 观察实际连接

在一个终端观察 7891：

```bash
ssh jmk 'watch -n 0.2 "ss -ntp | grep 127.0.0.1:7891"'
```

在另一个终端执行 `doctor` 或 `run`。应观察到 `x-monitor` 或其 Grok 子进程连接
`127.0.0.1:7891`，并由代理进程接收。当前 jmk 已分别验证：

- Telegram 诊断期间，`x-monitor -> 127.0.0.1:7891`；
- Grok 最小查询期间，`grok -> 127.0.0.1:7891`。

SQLite、文件锁和 gocron 都是本地操作，不产生外部流量。

## 5. 用户级 systemd hardening 说明

用户级 unit 与系统级 unit 的权限能力不同。当前用户级 unit 保留：

```ini
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
RestrictSUIDSGID=true
ReadWritePaths=%h/.local/share/x-monitor %h/.grok
```

`%h/.grok` 必须可写，因为 Grok 可能刷新认证文件。部署测试中，以下设置在该 Ubuntu
22.04 用户级 manager 上导致 `status=218/CAPABILITIES`，因此只在系统级 unit 中
使用：

```ini
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
LockPersonality=true
```

这不是删除安全措施的通用建议，而是用户级 systemd 的能力边界。修改 hardening 后
必须重新执行 `systemctl --user daemon-reload` 并检查服务状态。

## 6. 系统级 systemd 部署

拥有 root 权限时，可以按照 [中文 README](../README.zh-CN.md) 使用：

- `/usr/local/bin/x-monitor`；
- `/etc/x-monitor/config.toml`；
- `/etc/x-monitor/x-monitor.env`；
- `/var/lib/x-monitor`；
- `deploy/systemd/x-monitor.service`。

系统级 unit 的 `User=x-monitor`，安全限制更完整。systemd 仍然只守护进程；禁止
另外创建 systemd timer 或 Linux cron，否则会产生重复调度。

## 7. 升级和回滚

升级前：

1. 记录当前二进制 SHA-256 和版本；
2. 备份 `state.db`，同时保留其 WAL/SHM 文件，或先停服务后复制数据库；
3. 本地执行 `go test ./...`、`go test -race ./...` 和 `go vet ./...`；
4. 构建并核验新的 Linux 二进制。

用户级部署的安全升级顺序：

```bash
ssh jmk 'systemctl --user stop x-monitor.service'
scp bin/x-monitor-linux-amd64 jmk:/tmp/x-monitor.new
ssh jmk 'install -m 0755 /tmp/x-monitor.new "$HOME/.local/bin/x-monitor"'
ssh jmk 'set -a; . "$HOME/.config/x-monitor/x-monitor.env"; set +a; \
  "$HOME/.local/bin/x-monitor" \
    --config "$HOME/.config/x-monitor/config.toml" migrate'
ssh jmk 'systemctl --user start x-monitor.service'
```

如果新版本无法启动，重新安装上一版二进制并启动服务。不要删除 SQLite；去重状态和
待投递 Outbox 都在数据库中。

## 8. 故障排查

| 现象 | 检查项 |
|---|---|
| `grok` 检查失败 | 绝对路径、版本、同用户登录、`auth.json` 权限、7891 代理 |
| Telegram 401 | Bot Token 错误或已被撤销 |
| Telegram 400 | chat ID 错误、Bot 未加入目标群组/频道或无发送权限 |
| 没有消息 | `status`、搜索窗口、是否确有新 status URL、Outbox 状态 |
| 重复消息 | 检查是否运行了第二个实例；极窄的发送后崩溃窗口允许一次重复 |
| `218/CAPABILITIES` | 用户级 unit 使用了不受支持的 capability/hardening 指令 |
| 服务重启后不运行 | `Linger=yes`、user manager、unit enable 状态和 journal |
| 代理配置存在但不通 | 检查 7891 监听、进程环境和实际 TCP 连接，不只看配置文件 |
