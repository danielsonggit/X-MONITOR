# x-monitor 设计文档

本目录保存 `x-monitor` 的系统设计、技术选型和实施计划。`x-monitor` 是一个独立的 Go 常驻服务，通过 Grok CLI 查询公开 X/Twitter 内容，使用 SQLite 保存状态并通过 Telegram Bot 推送新内容。

## 文档索引

1. [系统架构设计](01-system-architecture.md)
2. [技术选型](02-technology-selection.md)
3. [实施计划与验收标准](03-implementation-plan.md)
4. [设计决策与目录命名](04-design-decisions.md)
5. [Linux 部署指南](05-deployment-guide.md)

## 当前状态

- 项目状态：已实现、通过测试，并已在 Linux 用户级 systemd 环境完成部署验证
- 目标账户：`@CryptoDinduz`、`@2442lll`、`@iruletrenches`、`@btc_eth_owner`、`@yeonwoo1102`、`@xtony1314`、`@Crypto_Cat888`、`@AIonBase_`、`@blu0222`、`@ShawnThread`
- 调度频率：每小时
- 调度实现：Go 常驻进程内的 `go-co-op/gocron/v2`
- 内容范围：原创、回复、转帖，不设置关键词过滤
- 去重依据：规范化后的 X status URL
- 通知规则：仅有尚未报告的新帖子时发送 Telegram
- 部署方式：Go 单进程常驻服务；systemd 负责守护，gocron 负责进程内调度
- 网络出口：Grok CLI 和 Telegram 可通过标准代理变量统一走本机代理
