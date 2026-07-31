# x-monitor

`x-monitor` is a modular Go service that monitors selected public X accounts
through the locally authenticated Grok CLI and sends new posts to Telegram.

The primary documentation is available in
[README.zh-CN.md](README.zh-CN.md).

Key properties:

- in-process hourly scheduling with gocron, not Linux cron;
- Grok `x_search` executed in a private, restricted environment;
- canonical X status URL deduplication in SQLite;
- transactional Telegram outbox with leases and retries;
- first-run and downtime recovery windows;
- no Codex runtime or local LLM required.

Quick verification:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/x-monitor
```

The service requires a working Grok CLI login and a Telegram Bot token for
real end-to-end operation.
