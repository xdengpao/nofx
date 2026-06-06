# NOFX 量化交易系统 - 统一构建编排器

BINARY_NAME := nofx
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)

.PHONY: build build-quick run test test-backend test-frontend test-coverage dev docker-up docker-down clean install check-native-chanlunv2 native-chanlunv2 test-chanlunv2 test-chanlunv2-go drl-train drl-backtest drl-export

## 后端编译（注入版本信息）
build:
	go mod download
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

## 快速编译（无版本注入，用于开发迭代）
build-quick:
	go build -o $(BINARY_NAME) .

## 本地运行后端
run: build
	./$(BINARY_NAME)

## 全量测试（后端 + 前端）
test: test-backend test-frontend

## 后端测试
test-backend:
	go test ./...

## 前端测试
test-frontend:
	cd web && npm run test

## 带覆盖率的后端测试
test-coverage:
	go test -coverprofile=coverage.out ./...

## 前端开发服务器提示
dev:
	@echo "请手动启动前端开发服务器："
	@echo "  cd web && npm run dev"

## Docker Compose 构建并启动
docker-up:
	docker compose up -d --build

## Docker Compose 停止
docker-down:
	docker compose down

## 清理构建产物
clean:
	rm -f $(BINARY_NAME)
	rm -rf web/dist/

## 安装所有依赖（Go + npm）
install:
	go mod download
	cd web && npm ci

## 检查 Chanlun V2 native 依赖状态（只读）
check-native-chanlunv2:
	./scripts/check-chanlun-v2-native.sh

## 构建 Chanlun V2 Rust 静态库到 chanlun_v2/target/release
native-chanlunv2:
	./scripts/prepare-chanlun-v2-native.sh

## 使用 CGO 链接 Chanlun V2 native 库运行测试
test-chanlunv2:
	CGO_LDFLAGS="-L$(PWD)/chanlun_v2/target/release" go test ./strategy/chanlunv2

## 仅验证 Chanlun V2 Go 层逻辑，不替代生产 native 链接验证
test-chanlunv2-go:
	CGO_ENABLED=0 go test ./strategy/chanlunv2

## 训练 DRL/PPO 模型（参数通过 DRL_TRAIN_ARGS 传入）
drl-train:
	cd training/drl && python scripts/train.py $(DRL_TRAIN_ARGS)

## 运行 DRL 回测（参数通过 DRL_BACKTEST_ARGS 传入）
drl-backtest:
	go run ./cmd/backtest $(DRL_BACKTEST_ARGS)

## 导出 DRL/PPO ONNX 模型（参数通过 DRL_EXPORT_ARGS 传入）
drl-export:
	cd training/drl && python scripts/export_model.py $(DRL_EXPORT_ARGS)
