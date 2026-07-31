# x-monitor 设计决策与目录命名

本文解释当前代码为什么使用 `domain`、`application`、`ports`、`adapters` 和
`infrastructure` 等名称，以及这种结构对本项目的实际收益、代价和后续调整边界。

## 1. 结论

当前实现是一个模块化单体，不是微服务。它采用了六边形架构的依赖方向，并用
Uber Fx 管理进程生命周期：

```text
commands / scheduler
        │
        ▼
application ──► ports ◄── adapters
        │                    │
        ▼                    ▼
      domain              外部系统
                         Grok / SQLite / Telegram / 文件锁
```

核心规则是：业务层可以定义外部能力需要满足的接口，但不能反向依赖 Grok、
Telegram、SQLite、gocron 或某个具体 SDK。

这套结构对当前项目而言偏重，但不是无意义的复杂化。它主要解决三个真实问题：

1. Grok CLI 是外部进程，输出不可信，且未来可能更换调用方式。
2. Telegram 投递需要持久化、重试和崩溃恢复，不能和搜索逻辑绑死。
3. 去重、首次运行窗口和恢复窗口属于业务规则，不能散落在 CLI、数据库和定时器中。

## 2. 各目录的含义

### 2.1 `internal/domain`

`domain` 不是“域名”，而是业务领域。这里放系统最稳定的业务概念和规则，例如：

- `Post`、`Run`、`Delivery`、`SearchWindow`；
- 原创、回复、转帖等内容类型；
- X status URL 规范化；
- 运行状态和投递状态。

该目录不得导入 Telegram SDK、SQLite 驱动、gocron 或 Grok 执行代码。这样可以在
不启动数据库、不访问网络的情况下测试核心规则。

### 2.2 `internal/application`

`application` 表示应用用例，而不是 HTTP 应用。它负责把多个业务动作编排成一次
完整操作，例如：

- 计算本轮搜索窗口；
- 获取运行锁；
- 调用搜索器；
- 验证、规范化和去重帖子；
- 在一个事务中写入帖子和待投递记录；
- 发送或重试 Telegram 消息；
- 完成本轮运行并推进成功游标。

这里回答的是“完成一次监控需要哪些步骤”，不负责 Grok 命令行参数、SQL 语句或
Telegram HTTP 细节。

### 2.3 `internal/ports`

`ports` 是业务层与外部世界之间的接口边界，例如 `Searcher`、`Notifier`、
`RunRepository` 和 `RunLocker`。

接口放在核心侧的原因是：由使用者定义它需要什么能力，而不是由某个 SDK 决定
业务代码长什么样。测试可以用内存 fake 替代 Grok、Telegram 和 SQLite。

### 2.4 `internal/adapters`

`adapters` 是端口的具体实现：

- `grok`：安全启动 Grok CLI，限制环境变量和工具权限，解析返回结果；
- `sqlite`：迁移、SQL 查询和事务；
- `telegram`：格式化并发送 Bot 消息；
- `filelock`：跨进程运行锁。

这些模块可以依赖第三方库，但不能把第三方类型泄漏到业务层。

### 2.5 `internal/infrastructure`

`infrastructure` 放进程级、跨业务的技术能力：

- `config`：TOML、环境变量覆盖和配置校验；
- `logging`：结构化日志；
- `scheduler`：进程内 gocron 调度；
- `buildinfo`：版本、commit 和构建时间。

它与 `adapters` 的边界并非数学定理。本项目采用的判断标准是：实现某个业务端口的
代码放 `adapters`；支撑整个进程运行、但不实现业务端口的代码放
`infrastructure`。

### 2.6 `internal/app` 与 `internal/commands`

`app` 只负责把 Fx 模块组合起来，是 composition root；`commands` 负责 Cobra CLI
入口。二者都应保持薄，不承载监控规则。

## 3. 为什么不按第三方库划分业务

如果把代码直接组织成 `sqlite/`、`telegram/`、`gocron/` 和 `grok/`，短期更容易
找到 SDK 调用，长期却容易让业务流程散落在技术模块里。更换搜索来源、增加第二个
通知渠道或测试恢复窗口时，会同时修改多个不相关目录。

当前分层让变化方向更清晰：

| 变化 | 主要修改位置 |
|---|---|
| 改首次运行通知窗口 | `domain` / `application` |
| 更换 Grok CLI 参数或解析格式 | `adapters/grok` |
| 更换 Telegram SDK | `adapters/telegram` |
| 修改表结构 | `adapters/sqlite` |
| 修改每小时触发规则 | `infrastructure/scheduler` 和配置 |
| 新增 CLI 子命令 | `commands` |

## 4. 这套结构的代价

代价同样真实：

- 目录和接口数量比简单脚本多；
- 阅读一次调用链需要跨越 application、port 和 adapter；
- Fx 的依赖注入降低了构造代码的显式程度；
- 对只有一个实现、不会被替换的能力，接口可能暂时显得多余。

因此不应继续为了“架构完整”机械增加层级。新增抽象必须至少满足以下条件之一：

1. 隔离真实外部系统或第三方 SDK；
2. 需要 fake 以进行可靠测试；
3. 存在事务、重试、安全或生命周期边界；
4. 已经有第二个实现，或近期确定会有。

## 5. 是否改成 feature-first

如果项目长期只保留“单组账户、单个 Telegram 目标、单实例”这一个用例，可以在
不改变依赖方向的前提下压平目录：

```text
internal/
├── monitor/
├── delivery/
├── xsearch/
├── telegram/
├── storage/
├── scheduler/
├── config/
└── logging/
```

当前不为目录美观进行重构。现有代码已经实现并经过测试，改名本身不增加业务能力，
却会制造大量文件移动和审查噪声。只有出现以下情况之一才重新评估：

- 新成员普遍无法快速定位代码；
- 某一功能的修改持续跨越过多层；
- 新增多个监控任务或多个通知渠道后，按功能聚合明显更清晰；
- Fx 和接口样板的维护成本超过其测试与隔离收益。

## 6. 已确认的关键设计决策

- 使用单个 Go 常驻进程，不拆微服务。
- 使用 gocron 进程内调度；systemd 只负责启动、停止和崩溃重启。
- 使用 SQLite 作为单机关系型数据库，并开启 WAL、foreign keys 和 busy timeout。
- 以规范化后的 X status URL 作为帖子唯一标识。
- 帖子和 Outbox 待发送记录在同一事务中创建。
- Telegram 采用至少一次投递；极窄的崩溃窗口可能造成一次重复消息。
- Grok CLI 是唯一需要访问大模型服务的组件；x-monitor 本身不接入模型 API，运行时
  不需要 Codex。
- Grok 结果是不可信输入，只按受限 JSON 解析，绝不作为本地命令执行。
- Grok CLI 使用固定绝对路径、隔离 HOME、环境变量白名单和工具白名单。
- 第一版采用单机部署，不实现多节点选主或分布式锁。
