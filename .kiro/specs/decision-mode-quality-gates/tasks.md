# Decision Mode Quality Gates Tasks

## Phase 1: 后端模式标签与上下文传递

- [x] 1. 修正 AutoTrader 决策模式标签
  - 更新 `trader/auto_trader.go` 的 `decisionModeLabel()`，覆盖 `ai`、`programmatic`、`chanlun_v2` 和 unknown mode。
  - 更新 `AutoTrader.Run()` 启动日志，使 `chanlun_v2` 显示为“缠论V2策略自动交易系统启动”和对应策略说明。
  - 更新 `runCycle()` 中调用 `getFullDecision(ctx)` 前的动作日志，使 `chanlun_v2` 显示为正在运行缠论 V2 策略，而不是“正在请求AI分析并决策”。
  - 验证点：`chanlun_v2` 周期日志不再出现“AI决策周期/AI决策分析摘要/正在请求AI分析并决策”。

- [x] 2. 将决策模式注入 decision.Context
  - 在 `decision.Context` 增加 `DecisionMode string` 字段，保持 `json:"-"`。
  - 在 `AutoTrader.buildTradingContext()` 设置 `DecisionMode: at.GetDecisionMode()`。
  - 验证点：open gate 可从 `ctx.DecisionMode` 获取当前模式。

- [x] 3. 补充 trader 层测试
  - 为 `decisionModeLabel()` 增加 `ai`、空 mode、`programmatic`、`chanlun_v2`、unknown mode 测试。
  - 扩展 `TestBuildTradingContext_InjectsAIStateRiskAndExecutionQuality` 或新增测试，确认 `ctx.DecisionMode` 被注入。
  - 验证命令：`CGO_ENABLED=0 go test ./trader`。

## Phase 2: 模式感知执行质量统计

- [x] 4. 扩展 ExecutionQualityStats 数据结构
  - 在 `logger.ExecutionQualityStats` 中新增可选字段：
    - `AIFailureCountByMode map[string]int`
    - `StrategyFailureCount int`
    - `StrategyFailureCountByMode map[string]int`
  - 保留现有 `AIFailureCount int json:"ai_failure_count"` 字段，确保旧 API/前端兼容。
  - 验证点：旧日志 JSON 仍可反序列化，旧字段仍输出。

- [x] 5. 实现 mode-aware 失败分类 helper
  - 在 `logger/decision_logger.go` 增加内部 helper：
    - `normalizeDecisionModeForQuality(mode string) string`
    - `isExplicitAIProviderFailureText(value string) bool`
    - `isAIFailureRecord(record *DecisionRecord) bool`
    - `isStrategyFailureRecord(record *DecisionRecord) bool`
  - 规则：
    - `decision_mode == "ai"` 或空 legacy mode，且错误文本匹配 AI/API/解析失败时计入 AI failure。
    - `decision_mode == "programmatic"` 或 `chanlun_v2` 的通用周期级失败计入 strategy failure，不计入 AI failure。
    - 非 AI mode 只有在错误文本明确指向 AI provider/API 失败时才计入 AI failure；通用旧文案“获取AI决策失败”不得覆盖 `decision_mode`。
    - 风控拒绝和执行动作失败继续走现有 action-level 统计。
  - 验证点：混合历史记录能区分 AI 调用失败和策略失败。

- [x] 6. 更新 BuildExecutionQuality 统计逻辑
  - 用新 helper 替换当前仅靠 `isAIFailureText(record.ErrorMessage)` 的统计。
  - 填充 `AIFailureCountByMode`、`StrategyFailureCount`、`StrategyFailureCountByMode`。
  - 保持 `OpenRejectedCount`、`ProtectionOrderFailures`、`HighRiskExecutionFailures` 等现有统计行为不变。
  - 验证命令：`CGO_ENABLED=0 go test ./logger`。

- [x] 7. 补充 logger 层测试
  - 覆盖 AI 模式失败计入 `AIFailureCount`。
  - 覆盖 legacy 空 mode 旧日志仍计入 `AIFailureCount`。
  - 覆盖 `chanlun_v2` 记录包含旧文案“获取AI决策失败”时不计入 AI failure，而计入 strategy failure。
  - 覆盖非 AI 记录包含明确 AI provider/API 失败文本时计入 AI failure。
  - 覆盖 `programmatic` 策略失败不影响 AI failure。
  - 验证命令：`CGO_ENABLED=0 go test ./logger`。

## Phase 3: Open Gate 模式感知降权

- [x] 8. 修改 execution quality gate 入参
  - 将 `applyExecutionQualityGate(result, quality)` 调整为 `applyExecutionQualityGate(result, quality, decisionMode)`。
  - 在 `EvaluateOpenGate()` 中传入 `ctx.DecisionMode`。
  - 兼容 mode 为空的 legacy 行为。

- [x] 9. 限定 AI-only gate 只应用于 AI 模式
  - 当 `decisionMode == "ai"` 或 legacy 空 mode 时，`AIFailureCount >= 3` 才降权。
  - 将原因文案改为“AI调用失败次数偏高，新开仓降权”。
  - 当 `decisionMode == "programmatic"` 或 `chanlun_v2` 时跳过该 AI failure penalty。
  - 当 `decisionMode == "programmatic"` 或 `chanlun_v2` 时，`AIBackoffUntil` 不应阻断策略开仓；AI mode 和 legacy 空 mode 保持现有退避阻断。
  - 保留保护单失败、高危执行失败、partial close 失败对所有模式的现有阻断/降权。
  - 验证点：非 AI 策略不会再出现“AI失败次数偏高”。

- [x] 10. 补充 decision 层测试
  - 覆盖 AI 模式下 `AIFailureCount >= 3` 会降权并降低 effective risk。
  - 覆盖 `programmatic` 模式下同样的 `AIFailureCount` 不降权。
  - 覆盖 `chanlun_v2` 模式下同样的 `AIFailureCount` 不降权。
  - 覆盖 `AIBackoffUntil` 只阻断 AI/legacy mode，不阻断 `programmatic` 或 `chanlun_v2`。
  - 覆盖保护单失败或高危执行失败在所有模式下仍阻断。
  - 验证命令：`CGO_ENABLED=0 go test ./decision`。

## Phase 4: API/前端兼容展示

- [x] 11. 更新前端 ExecutionQuality 类型
  - 在 `web/src/types/index.ts` 和 `web/src/types.ts` 为新增统计字段添加 optional 类型：
    - `ai_failure_count_by_mode?: Record<string, number>`
    - `strategy_failure_count?: number`
    - `strategy_failure_count_by_mode?: Record<string, number>`
  - 同步更新 `web/src/components/AILearning.tsx` 内联 `execution_quality` 类型，避免组件局部类型与共享类型漂移。
  - 保持 `ai_failure_count` 必填或现有兼容写法不变。
  - 验证命令：`cd web && npm run build`。

- [x] 12. 优化前端执行质量文案
  - 在 `web/src/components/AILearning.tsx` 将 “AI失败” 文案调整为 “AI调用失败”。
  - 如展示 per-mode 详情，使用紧凑、可选方式，不影响没有新字段的旧响应。
  - 验证点：前端可区分 AI 调用失败和策略失败，不因 optional 字段缺失报错。

## Phase 5: 集成验证与回归

- [x] 13. 运行后端 targeted tests
  - 执行：
    - `CGO_ENABLED=0 go test ./decision ./logger ./trader`
    - `CGO_ENABLED=0 go test ./api ./manager`
  - 验证点：模式标签、执行质量统计、open gate 行为均通过测试。

- [x] 14. 运行全量构建
  - 执行：`CGO_ENABLED=0 go build ./...`
  - 如果前端文件被修改，执行：`cd web && npm run build`。
  - 验证点：后端编译和前端类型检查通过。

- [x] 15. 本地日志语义自检
  - 检查关键文案：
    - `chanlun_v2` 使用“缠论V2策略周期”。
    - `programmatic` 使用“程序化策略周期”。
    - 非 AI open rejection 不包含“AI失败次数偏高”。
    - 非 AI open rejection 不因 `AIBackoffUntil` 出现“AI调用退避中”。
    - AI mode 保留“AI调用失败次数偏高”。
  - 验证点：用户能从日志直接判断当前策略模式和 gate 来源。

## Phase 6: 提交与部署

- [x] 16. 代码审查与提交
  - 检查 `git diff --check`。
  - 确认没有提交 `data/`、`decision_logs/`、`coin_pool_cache/` 或真实密钥。
  - 提交信息建议：`Clarify decision mode quality gates`。

- [ ] 17. 推送 GitHub
  - 推送当前分支 `jzhbnofxdev` 到 GitHub。
  - 验证点：远端包含新提交。

- [ ] 18. 部署到 161 服务器
  - 在 161 仓库确认工作区状态，保留既有未跟踪 Rust 构建产物，不误删用户/服务器改动。
  - 拉取最新提交。
  - 执行 `sudo docker compose up -d --build`。
  - 验证 `nofx-trading` 和 `nofx-frontend` 均 healthy。

- [ ] 19. 部署后线上验证
  - 检查 `/api/status` 和前端 HTTP 状态。
  - 查看 `nofx-trading` 最近日志，确认：
    - `Aster Chanlun V2 Trader` 周期显示为缠论 V2 策略。
    - `Aster DeepSeek Trader` 程序化周期显示为程序化策略。
    - 非 AI 策略开仓拒绝原因不再出现误导性 AI failure penalty。
