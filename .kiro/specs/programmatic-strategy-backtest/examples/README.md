# 本地程序化策略回测示例

本功能只用于开发测试环境，不部署到 161。

```bash
go run ./cmd/history-data fetch \
  --db backtest_data/nofx_history.sqlite \
  --source binance-futures \
  --symbols BTCUSDT,ETHUSDT \
  --timeframes 3m,15m,1h,4h \
  --data-from 2026-01-01 \
  --data-to 2026-02-01 \
  --timezone Asia/Singapore \
  --requests-per-minute 120 \
  --concurrency 1

go run ./cmd/history-data inspect --db backtest_data/nofx_history.sqlite

go run ./cmd/backtest run \
  -config .kiro/specs/programmatic-strategy-backtest/examples/backtest.example.json

go run ./cmd/backtest batch \
  -config .kiro/specs/programmatic-strategy-backtest/examples/batch.example.json
```

本地页面需要显式开启：

```bash
NOFX_BACKTEST_API_ENABLED=true go run main.go
cd web && npm run dev
```

生产 `docker-compose.yml`、`start.sh` 和 161 部署流程不启用该页面和 API。
