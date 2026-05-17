.PHONY: all build test cover vet lint clean examples help

# 默认目标
all: vet build test

# ── 构建 ──────────────────────────────────────────────────────────────────────

## build: 编译主包
build:
	go build ./...

## examples: 编译所有示例
examples:
	go build ./examples/...

# ── 测试 ──────────────────────────────────────────────────────────────────────

## test: 运行单元测试（60 秒超时）
test:
	go test -timeout 60s ./...

## cover: 运行测试并生成覆盖率报告（目标 ≥ 70%）
cover:
	go test -timeout 60s -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## cover-html: 在浏览器中打开 HTML 覆盖率报告
cover-html: cover
	go tool cover -html=coverage.out

# ── 代码质量 ──────────────────────────────────────────────────────────────────

## vet: 运行 go vet 静态检查
vet:
	go vet ./...

## lint: 运行 golangci-lint（需提前安装：go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest）
lint:
	golangci-lint run ./...

# ── 清理 ──────────────────────────────────────────────────────────────────────

## clean: 删除构建产物和覆盖率文件
clean:
	go clean ./...
	rm -f coverage.out

# ── 帮助 ──────────────────────────────────────────────────────────────────────

## help: 列出所有可用目标
help:
	@echo "可用目标："
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
