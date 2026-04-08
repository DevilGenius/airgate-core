#!/usr/bin/env bash
# ============================================================
# AirGate Core 一行安装脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/DouDOU-start/airgate-core/master/deploy/install.sh | bash
#
# 或带参数（先 download 再执行更安全可审计）：
#   curl -fsSL https://raw.githubusercontent.com/DouDOU-start/airgate-core/master/deploy/install.sh -o install.sh
#   bash install.sh
#
# 环境变量覆盖（可选）：
#   AIRGATE_DIR     安装目录            默认: ./airgate
#   AIRGATE_PORT    HTTP 端口           默认: 9517
#   AIRGATE_BRANCH  从 GitHub 拉哪个分支 默认: master
#   AIRGATE_TAG     固定镜像版本         默认: latest
#   NON_INTERACTIVE 跳过所有交互（CI 用） 默认: 0
# ============================================================

set -euo pipefail

# ---- 颜色输出 ----
if [[ -t 1 ]]; then
  C_RESET='\033[0m'; C_BOLD='\033[1m'; C_DIM='\033[2m'
  C_RED='\033[31m'; C_GREEN='\033[32m'; C_YELLOW='\033[33m'; C_BLUE='\033[34m'; C_CYAN='\033[36m'
else
  C_RESET=''; C_BOLD=''; C_DIM=''; C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_CYAN=''
fi

info()    { printf '%b\n' "${C_CYAN}==>${C_RESET} $*"; }
ok()      { printf '%b\n' "${C_GREEN}✓${C_RESET} $*"; }
warn()    { printf '%b\n' "${C_YELLOW}!${C_RESET} $*"; }
err()     { printf '%b\n' "${C_RED}✗${C_RESET} $*" >&2; }
section() { printf '\n%b\n' "${C_BOLD}${C_BLUE}── $* ──${C_RESET}"; }

# ---- 横幅 ----
cat <<'BANNER'

    ╔═══════════════════════════════════════╗
    ║         AirGate Core Installer        ║
    ║   Pluggable AI Gateway · One-liner    ║
    ╚═══════════════════════════════════════╝

BANNER

# ---- 默认参数 ----
AIRGATE_DIR="${AIRGATE_DIR:-./airgate}"
AIRGATE_PORT="${AIRGATE_PORT:-9517}"
AIRGATE_BRANCH="${AIRGATE_BRANCH:-master}"
AIRGATE_TAG="${AIRGATE_TAG:-latest}"
NON_INTERACTIVE="${NON_INTERACTIVE:-0}"

REPO_RAW_URL="https://raw.githubusercontent.com/DouDOU-start/airgate-core/${AIRGATE_BRANCH}/deploy"

# ---- 依赖检查 ----
section "环境检查"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    err "未找到命令: $1${2:+，请先安装 $2}"
    exit 1
  fi
  ok "$1"
}

require_cmd docker "Docker"

# 兼容 docker-compose 老版本（v1）和 docker compose 新插件（v2）
if docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
  ok "docker compose (v2)"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
  ok "docker-compose (v1, 兼容模式)"
else
  err "未找到 docker compose / docker-compose，请安装 Docker Compose"
  exit 1
fi

require_cmd curl
require_cmd openssl

# ---- 交互式参数 ----
prompt() {
  local var=$1 msg=$2 default=$3 reply
  if [[ "$NON_INTERACTIVE" == "1" ]]; then
    eval "$var=\"\$default\""
    return
  fi
  printf '%b' "${C_DIM}?${C_RESET} ${msg} ${C_DIM}[${default}]${C_RESET}: " > /dev/tty
  read -r reply < /dev/tty || reply=""
  eval "$var=\"\${reply:-\$default}\""
}

section "安装参数"
prompt AIRGATE_DIR  "安装目录"           "$AIRGATE_DIR"
prompt AIRGATE_PORT "HTTP 监听端口"       "$AIRGATE_PORT"
prompt AIRGATE_TAG  "镜像版本 (latest 或 v0.x.y)" "$AIRGATE_TAG"

# 把 ~ 展开
AIRGATE_DIR="${AIRGATE_DIR/#\~/$HOME}"

# ---- 准备目录 ----
section "准备目录 ${AIRGATE_DIR}"

if [[ -e "$AIRGATE_DIR" ]] && [[ -n "$(ls -A "$AIRGATE_DIR" 2>/dev/null || true)" ]]; then
  warn "目录 ${AIRGATE_DIR} 已存在且非空"
  if [[ -f "$AIRGATE_DIR/docker-compose.yml" ]]; then
    if [[ "$NON_INTERACTIVE" == "1" ]]; then
      err "已存在 docker-compose.yml，请先备份或删除目录后重试（NON_INTERACTIVE 模式不会覆盖）"
      exit 1
    fi
    printf '%b' "${C_YELLOW}?${C_RESET} 检测到已有部署，是否覆盖 docker-compose.yml？.env 与 ./data 不会被动 [y/N]: " > /dev/tty
    read -r ans < /dev/tty || ans=""
    if [[ "$ans" != "y" && "$ans" != "Y" ]]; then
      info "已取消安装。如需重新部署，请清空 ${AIRGATE_DIR} 后重试。"
      exit 0
    fi
  fi
fi

mkdir -p "$AIRGATE_DIR"
cd "$AIRGATE_DIR"
mkdir -p data/postgres data/redis data/plugins data/uploads

ok "目录就绪：$(pwd)"

# ---- 下载 docker-compose.yml ----
section "下载 docker-compose.yml"
curl -fsSL "${REPO_RAW_URL}/docker-compose.yml" -o docker-compose.yml
ok "已下载到 $(pwd)/docker-compose.yml"

# ---- 生成 / 复用 .env ----
section "生成 .env"

if [[ -f .env ]]; then
  warn ".env 已存在，沿用现有密钥（不会覆盖）"
else
  DB_PASSWORD="$(openssl rand -hex 24)"
  REDIS_PASSWORD="$(openssl rand -hex 24)"
  JWT_SECRET="$(openssl rand -hex 32)"

  cat > .env <<EOF
# AirGate Core 部署配置（由 install.sh 自动生成于 $(date '+%Y-%m-%d %H:%M:%S')）
# 任何修改需要 docker compose up -d 重启生效
# 数据库 / Redis 密码相关说明见 README.md

AIRGATE_IMAGE=ghcr.io/doudou-start/airgate-core
AIRGATE_IMAGE_TAG=${AIRGATE_TAG}

PORT=${AIRGATE_PORT}
BIND_HOST=0.0.0.0
TZ=Asia/Shanghai

DB_PASSWORD=${DB_PASSWORD}
REDIS_PASSWORD=${REDIS_PASSWORD}
JWT_SECRET=${JWT_SECRET}

API_KEY_SECRET=
PLUGINS_MARKETPLACE_GITHUB_TOKEN=
EOF
  chmod 600 .env
  ok "已生成 .env（所有密钥随机，权限 600）"
fi

# ---- 拉镜像并启动 ----
section "拉取镜像并启动容器"
$COMPOSE pull
$COMPOSE up -d
ok "容器已启动"

# ---- 等待健康 ----
section "等待 core 就绪"
attempts=0
max_attempts=30
until curl -fsS "http://127.0.0.1:${AIRGATE_PORT}/healthz" >/dev/null 2>&1; do
  attempts=$((attempts + 1))
  if [[ $attempts -ge $max_attempts ]]; then
    warn "core 在 ${max_attempts}s 内未就绪，请用 '$COMPOSE logs -f core' 排查"
    break
  fi
  sleep 1
done
[[ $attempts -lt $max_attempts ]] && ok "core 已就绪"

# ---- 完成 ----
cat <<DONE

${C_GREEN}${C_BOLD}✓ 安装完成${C_RESET}

  访问地址 : ${C_CYAN}http://<your-host>:${AIRGATE_PORT}${C_RESET}
  安装目录 : ${C_CYAN}$(pwd)${C_RESET}
  数据目录 : ${C_CYAN}$(pwd)/data${C_RESET}
  环境变量 : ${C_CYAN}$(pwd)/.env${C_RESET}  ${C_DIM}(权限 600，含随机密钥)${C_RESET}

${C_BOLD}下一步${C_RESET}
  1. 浏览器打开上面的访问地址
  2. 系统安装向导 → 由于 .env 已配置好 DB / Redis，向导会自动跳过这两步，
     只需建立管理员账号即可
  3. 进入管理后台 → 插件管理 → 插件市场 → 按需安装插件

${C_BOLD}常用命令${C_RESET}
  $COMPOSE logs -f core      # 查看日志
  $COMPOSE restart           # 重启
  $COMPOSE down              # 停止
  $COMPOSE pull && $COMPOSE up -d   # 升级镜像

${C_DIM}文档：https://github.com/DouDOU-start/airgate-core${C_RESET}

DONE
