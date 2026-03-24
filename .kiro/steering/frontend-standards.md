---
inclusion: fileMatch
fileMatchPattern: "web/src/**/*.ts,web/src/**/*.tsx"
---

# 前端编码规范

## 技术栈

- React 18 + TypeScript + Vite + Tailwind CSS
- SWR 数据获取和缓存
- Recharts 图表库
- Binance 风格深色主题

## 组件规范

- 函数组件 + Hooks，不使用 class 组件
- 组件文件放在 `web/src/components/`
- 类型定义放在 `web/src/types/`
- API 调用封装在 `web/src/lib/api.ts`

## 数据刷新策略

- 账户数据: 15 秒刷新 (`refreshInterval: 15000`)
- 决策数据: 30 秒刷新 (`refreshInterval: 30000`)
- 图表最大 2000 数据点

## 国际化

- 使用 `web/src/i18n/translations.ts` 管理翻译
- 通过 `LanguageContext` 切换中英文
- 所有用户可见文本必须支持双语

## 样式规范

- 使用 Tailwind CSS utility classes
- 深色主题: 背景 `bg-gray-900`，卡片 `bg-gray-800`
- 盈利绿色 `text-green-400`，亏损红色 `text-red-400`
- Trader 颜色使用 `web/src/utils/traderColors.ts` 统一分配
