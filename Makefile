# AirGate Core Makefile

# 变量
BACKEND_DIR := backend
WEB_DIR := web
BINARY := $(BACKEND_DIR)/server
GO := GOTOOLCHAIN=local go

.PHONY: help dev dev-backend dev-frontend build build-backend build-frontend \
        ent lint clean install

help: ## 显示帮助信息
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ===================== 开发 =====================

dev: ## 同时启动前后端开发服务器
	@echo "启动开发环境..."
	@$(MAKE) dev-backend &
	@$(MAKE) dev-frontend
	@wait

dev-backend: ## 启动后端（带热重载，需要 air）
	@cd $(BACKEND_DIR) && \
	if command -v air > /dev/null 2>&1; then \
		air; \
	else \
		echo "未安装 air，使用普通模式启动（无热重载）"; \
		echo "安装 air: go install github.com/air-verse/air@latest"; \
		$(GO) run ./cmd/server; \
	fi

dev-frontend: ## 启动前端开发服务器
	@cd $(WEB_DIR) && npm run dev

# ===================== 构建 =====================

build: build-backend build-frontend ## 构建前后端

build-backend: ## 编译后端二进制
	@cd $(BACKEND_DIR) && $(GO) build -o server ./cmd/server
	@echo "后端编译完成: $(BINARY)"

build-frontend: ## 构建前端产物
	@cd $(WEB_DIR) && npm run build
	@echo "前端构建完成: $(WEB_DIR)/dist/"

# ===================== 代码生成 =====================

ent: ## 生成 Ent ORM 代码
	@cd $(BACKEND_DIR) && $(GO) generate ./ent
	@echo "Ent 代码生成完成"

# ===================== 质量检查 =====================

lint: ## 代码检查
	@cd $(BACKEND_DIR) && $(GO) vet ./...
	@echo "Go vet 通过"

# ===================== 依赖安装 =====================

install: ## 安装前后端依赖
	@cd $(BACKEND_DIR) && $(GO) mod download
	@cd $(WEB_DIR) && npm install
	@echo "依赖安装完成"

# ===================== 清理 =====================

clean: ## 清理构建产物
	@rm -f $(BINARY)
	@rm -rf $(WEB_DIR)/dist
	@echo "清理完成"
