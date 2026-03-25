# Product Overview

NOFX is an agentic trading operating system for cryptocurrency futures markets. It uses AI models (DeepSeek, Qwen, or custom OpenAI-compatible APIs) to autonomously make trading decisions on perpetual futures contracts.

## Core Concept

Multiple AI-powered traders compete in real-time, each managing its own account on supported exchanges (Binance Futures, Hyperliquid DEX, Aster DEX). The system runs on a configurable cycle (default 3 minutes) where each trader:

1. Collects market data and account state
2. Evaluates existing positions against trade plans
3. Calls the AI model for new opportunity analysis
4. Executes trading decisions (open/close/adjust positions)

## Key Capabilities

- Multi-agent competition: multiple traders with different AI models run simultaneously
- Multi-exchange support: Binance, Hyperliquid, Aster DEX via unified `Trader` interface
- AI self-learning: historical performance feedback (last 100 cycles) informs each decision
- Risk management: circuit breaker, position limits (max 3), risk budgets, correlation-adjusted sizing
- Multi-timeframe analysis: 3m, 15m, 1h, 4h candle data with technical indicators (EMA, MACD, RSI, ADX, Bollinger Bands, ATR)
- Trade plan lifecycle: structured plans with invalidation conditions, trailing stops, partial take-profit tranches
- Web dashboard: React frontend for real-time monitoring of equity curves, positions, and AI decision logs

## Primary Language

The codebase uses Chinese (中文) for log messages, comments, error messages, and variable descriptions. English is used for code identifiers, API endpoints, and documentation files.
