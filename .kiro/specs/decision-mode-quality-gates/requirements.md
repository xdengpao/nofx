# Decision Mode Quality Gates Requirements

## 背景

线上部署后，`chanlun_v2` 周期在日志中出现了两类误导信息：

1. `chanlun_v2` 被 `AutoTrader.decisionModeLabel()` 归类为“AI决策”，导致日志出现“AI决策周期”和“AI决策分析摘要”，但该周期实际由缠论 V2 策略引擎产生决策。
2. `decision/open_gate` 的执行质量 gate 复用 `logger.ExecutionQualityStats.AIFailureCount`，当历史日志中 AI/API/解析失败次数偏高时，程序化策略和缠论 V2 开仓也会出现“AI失败次数偏高，新开仓降权”。这条提示来自历史执行质量统计，不代表当前周期调用了 AI。

这两个问题不会直接绕过风控，但会降低运维可解释性，容易让用户误判当前策略模式、开仓拒绝原因和故障归属。

## 功能摘要

本优化要求让决策模式、执行质量统计和 open gate 原因具备模式感知能力：

- 日志、决策记录、前端/API 展示应准确区分 `ai`、`programmatic`、`chanlun_v2`。
- 历史 AI 调用失败不应默认影响非 AI 策略的新开仓 gate。
- 共享执行质量 gate 仍应保留真正跨模式的安全拦截，例如保护单失败、高危执行失败、partial close 失败。
- 历史字段兼容已有决策日志，避免破坏 replay、performance API 和前端读取。

## 术语

- **Decision Mode**：trader 的决策模式，当前包括 `ai`、`programmatic`、`chanlun_v2`。
- **AI 调用失败**：AI provider/API 调用、响应解析等只属于 AI 决策链路的失败。
- **明确 AI provider/API 失败文本**：错误文本直接指向 AI provider、AI API、AI 响应解析或具体 AI provider（如 DeepSeek/Qwen/OpenAI-compatible）失败。
- **通用旧 AI 文案**：旧日志中由模式标签误写产生的周期级文案，例如非 AI 策略记录里的“获取AI决策失败”。这类文案不得覆盖 `decision_mode` 的真实模式。
- **执行质量 gate**：`decision/open_gate.go` 中基于历史执行质量对新开仓进行阻断或降权的规则。
- **模式感知统计**：统计值需要保留失败所属的决策模式，并且 gate 根据当前 trader 模式选择是否应用。

## Requirements

### Requirement 1: 决策模式标签必须准确

**User Story:** 作为运维用户，我希望日志和界面能准确显示 trader 当前使用的决策模式，以便判断本轮决策来自 AI、程序化策略还是缠论 V2。

#### Acceptance Criteria

1. WHEN `DecisionMode == "programmatic"` THEN 后端周期日志 SHALL 使用“程序化策略”作为周期标签。
2. WHEN `DecisionMode == "chanlun_v2"` THEN 后端周期日志 SHALL 使用“缠论V2策略”作为周期标签，而不是“AI决策”。
3. WHEN `DecisionMode` 为空或为传统 AI 模式 THEN 后端周期日志 SHALL 保持“AI决策”语义。
4. WHEN trader 启动时 `DecisionMode == "chanlun_v2"` THEN 启动日志 SHALL 明确说明“缠论V2策略将生成交易决策”，不得输出“AI将全权决定...”。
5. IF 新增或未知 `DecisionMode` THEN 系统 SHALL 使用可读的 fallback 标签，并保留原始 `decision_mode` 字段用于排查。

### Requirement 2: AI 失败统计不得默认影响非 AI 策略

**User Story:** 作为策略开发者，我希望历史 AI provider 失败只影响 AI 决策模式，而不误伤程序化策略或缠论 V2 策略。

#### Acceptance Criteria

1. WHEN 当前 trader `DecisionMode != "ai"` AND 历史 `AIFailureCount >= 3` THEN open gate SHALL NOT 添加“AI失败次数偏高，新开仓降权”。
2. WHEN 当前 trader `DecisionMode == "ai"` AND 历史 AI 失败达到阈值 THEN open gate MAY 继续降权，并 SHALL 使用清晰的 AI 相关原因文案。
3. WHEN 非 AI 策略存在保护单失败、高危执行失败或 partial close 失败 THEN execution quality gate SHALL 继续按现有安全规则阻断或降权。
4. WHEN 当前 trader `DecisionMode != "ai"` AND `AIBackoffUntil` 仍处于未来时间 THEN open gate SHALL NOT 因 AI 调用退避阻断非 AI 策略开仓。
5. IF 当前决策模式缺失 THEN gate SHALL 按兼容逻辑处理，避免历史日志丢失字段导致 panic 或错误阻断。

### Requirement 3: 执行质量统计需要模式感知且兼容历史日志

**User Story:** 作为维护者，我希望 performance/replay 能区分失败来源，同时不破坏旧日志分析。

#### Acceptance Criteria

1. WHEN `logger.BuildExecutionQuality()` 分析决策日志 THEN 统计结果 SHALL 能区分 AI 调用失败、策略引擎失败、执行失败和风控拒绝。
2. WHEN 决策日志包含 `decision_mode` THEN AI 调用失败计数 SHALL 只计入 `ai` 模式，除非错误文本属于明确 AI provider/API 失败文本。
3. WHEN 决策日志缺少 `decision_mode` THEN 系统 SHALL 通过兼容启发式识别旧 AI 失败文本。
4. WHEN 非 AI 模式决策日志包含通用旧 AI 文案 THEN 统计 SHALL 优先相信 `decision_mode`，计入策略失败而不是 AI 调用失败。
5. Existing JSON field `ai_failure_count` SHALL remain available for backward compatibility.
6. New mode-aware fields, if added, SHALL use JSON tags and frontend TypeScript types that do not break older clients.

### Requirement 4: 风控原因文案必须表达真实来源

**User Story:** 作为交易监督者，我希望 open rejection 的原因能说明是行情/执行/AI provider/策略模式导致，而不是把所有问题都写成 AI。

#### Acceptance Criteria

1. WHEN execution quality gate 因 AI 调用失败降权 THEN 原因 SHALL 明确包含“AI调用失败”或等价措辞。
2. WHEN 非 AI 策略历史失败被展示，或未来被用于 gate 降权 THEN 原因 SHALL 使用“策略执行质量”或更具体的非 AI 文案，不得写“AI失败”；本次实现 SHALL 只新增策略失败可观测统计，不新增非 AI 策略失败降权规则。
3. WHEN open rejection 被写入 `DecisionRecord` THEN `StrategyMode`、`DecisionMode`、`SignalID`、`SignalType` 等已有策略元数据 SHALL 尽可能保留。
4. WHEN 前端展示最近决策或策略检查结果 THEN 用户 SHALL 能从现有字段或新增字段识别当前 trader 决策模式。

### Requirement 5: 缠论 V2 和程序化策略的安全边界不得降低

**User Story:** 作为系统负责人，我希望去除误导性 AI 降权不等于放松核心风控。

#### Acceptance Criteria

1. WHEN `chanlun_v2` 产生开仓信号 THEN 仍 SHALL 经过仓位补全、open gate、最终频率/持仓限制和执行 preflight。
2. WHEN `programmatic` 产生开仓或加仓信号 THEN 仍 SHALL 经过现有程序化策略验证链路。
3. WHEN BTC 多周期转弱、ADX/DI 不一致、下行结构、相关性集中、亏损模式等 gate 命中 THEN 系统 SHALL 保持现有拦截或降权行为。
4. IF 执行质量中存在保护单失败或高危执行失败 THEN 所有模式 SHALL 继续阻断新开仓。

### Requirement 6: 可验证性和回归覆盖

**User Story:** 作为开发者，我希望通过测试证明模式标签和 gate 逻辑不会再次混淆。

#### Acceptance Criteria

1. Unit tests SHALL cover `decisionModeLabel()` for `ai`、`programmatic`、`chanlun_v2` and unknown modes.
2. Unit tests SHALL cover `EvaluateOpenGate()` or its input path: AI failure count and AI backoff affect AI mode but not `programmatic` or `chanlun_v2` mode.
3. Logger tests SHALL cover mode-aware execution quality counting with mixed historical records.
4. Existing tests for `decision`、`logger`、`trader` SHALL continue passing.
5. If frontend types or display logic change, frontend build SHALL pass.
