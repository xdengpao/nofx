---
inclusion: fileMatch
fileMatchPattern: "config/**/*.go,config.json*"
---

# 配置参考

## config.json 结构

#[[file:config.json.example]]

## 配置验证规则

- `traders`: 至少 1 个，每个需要唯一 `id` 和非空 `name`
- `ai_model`: 必须是 `"qwen"` / `"deepseek"` / `"custom"`
- `exchange`: 必须是 `"binance"` / `"hyperliquid"` / `"aster"`，默认 `"binance"`
- Binance: 需要 `binance_api_key` + `binance_secret_key`
- Hyperliquid: 需要 `hyperliquid_private_key`
- Aster: 需要 `aster_user` + `aster_signer` + `aster_private_key`
- Custom AI: 需要 `custom_api_url` + `custom_api_key` + `custom_model_name`
- `initial_balance`: 必须 > 0
- `scan_interval_minutes`: 默认 3
- `api_server_port`: 默认 8080
- 杠杆默认 5 倍，超过 5 倍输出警告
- `use_default_coins=false` 且 `coin_pool_api_url=""` 时自动启用默认币种
