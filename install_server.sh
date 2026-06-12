#!/bin/bash
# install_server.sh - YALS Server Installer / Updater
#
# Builds from source: clones the repository, compiles the frontend + server,
# deploys the binary and web assets, then deletes the build sources.
# (Previously this downloaded a prebuilt binary from a mirror.)

set -e

SERVER_DIR="/etc/yals"
SERVER_BIN="/usr/bin/yals_server"
REPO_URL="${YALS_REPO_URL:-https://github.com/TogawaSakiko363/YALS.git}"
REPO_REF="${YALS_REPO_REF:-main}"
CONFIG_FILE="$SERVER_DIR/config.yaml"
SERVICE_FILE="/etc/systemd/system/yals.service"
WEB_DIR="$SERVER_DIR/web"

# 官方 tarball 的 Go 装在 /usr/local/go/bin、snap 装在 /snap/bin，这些目录通常不在
# sudo 的 secure_path 里，会导致"已装 Go 却检测为未安装"。显式补进 PATH。
export PATH="/usr/local/go/bin:/snap/bin:$PATH"

# ---- 依赖自检与自动安装 ----
GO_MIN_MAJOR=1
GO_MIN_MINOR=25
GO_FALLBACK_VERSION="go1.25.4" # 当无法获取最新版本号时使用
NODE_MIN_MAJOR=18

PKG_MGR=""
APT_UPDATED=0

detect_pkg_mgr() {
  for m in apt-get dnf yum pacman apk zypper; do
    if command -v "$m" >/dev/null 2>&1; then PKG_MGR="$m"; return 0; fi
  done
  return 1
}

pkg_install() {
  [ "$#" -gt 0 ] || return 0
  case "$PKG_MGR" in
    apt-get)
      if [ "$APT_UPDATED" -eq 0 ]; then apt-get update -y || true; APT_UPDATED=1; fi
      DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" ;;
    dnf) dnf install -y "$@" ;;
    yum) yum install -y "$@" ;;
    pacman) pacman -Sy --noconfirm "$@" ;;
    apk) apk add --no-cache "$@" ;;
    zypper) zypper install -y "$@" ;;
    *) echo "[WARN] 未识别的包管理器，无法自动安装: $*"; return 1 ;;
  esac
}

ensure_cmd() {
  local cmd="$1"; shift
  command -v "$cmd" >/dev/null 2>&1 && return 0
  echo "[INFO] 缺少 $cmd，尝试自动安装: $*"
  pkg_install "$@" || true
  command -v "$cmd" >/dev/null 2>&1
}

go_version_ok() {
  command -v go >/dev/null 2>&1 || return 1
  local v major minor
  v="$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')"
  major="$(echo "$v" | cut -d. -f1 | tr -cd '0-9')"
  minor="$(echo "$v" | cut -d. -f2 | tr -cd '0-9')"
  [ -n "$major" ] && [ -n "$minor" ] || return 1
  [ "$major" -gt "$GO_MIN_MAJOR" ] && return 0
  [ "$major" -eq "$GO_MIN_MAJOR" ] && [ "$minor" -ge "$GO_MIN_MINOR" ]
}

ensure_go() {
  if go_version_ok; then return 0; fi
  echo "[INFO] 未检测到满足要求的 Go (需 ${GO_MIN_MAJOR}.${GO_MIN_MINOR}+)，安装官方版本..."
  local arch goarch ver tmp
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) goarch="amd64" ;;
    aarch64|arm64) goarch="arm64" ;;
    armv6l|armv7l) goarch="armv6l" ;;
    *) echo "[ERROR] 不支持的架构: $arch，请手动安装 Go ${GO_MIN_MAJOR}.${GO_MIN_MINOR}+"; exit 1 ;;
  esac
  ver="$(curl -fsSL 'https://go.dev/VERSION?m=text' 2>/dev/null | head -n1)"
  [ -n "$ver" ] || ver="$GO_FALLBACK_VERSION"
  echo "[INFO] 下载 https://go.dev/dl/${ver}.linux-${goarch}.tar.gz"
  tmp="$(mktemp -d)"
  curl -fsSL "https://go.dev/dl/${ver}.linux-${goarch}.tar.gz" -o "$tmp/go.tar.gz"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$tmp/go.tar.gz"
  rm -rf "$tmp"
  export PATH="/usr/local/go/bin:$PATH"
  if [ -d /etc/profile.d ] && [ ! -f /etc/profile.d/go.sh ]; then
    echo 'export PATH=/usr/local/go/bin:$PATH' > /etc/profile.d/go.sh
  fi
  go_version_ok || { echo "[ERROR] Go 安装后仍不满足版本要求"; exit 1; }
  echo "[INFO] 已安装 $(go version)"
}

node_version_ok() {
  command -v node >/dev/null 2>&1 || return 1
  local v
  v="$(node -v 2>/dev/null | sed 's/^v//' | cut -d. -f1)"
  [ -n "$v" ] || return 1
  [ "$v" -ge "$NODE_MIN_MAJOR" ]
}

ensure_node() {
  if command -v npm >/dev/null 2>&1 && node_version_ok; then return 0; fi
  echo "[INFO] 未检测到满足要求的 Node.js (需 ${NODE_MIN_MAJOR}+)，开始安装..."
  if [ "$PKG_MGR" = "apt-get" ]; then
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash - || true
    APT_UPDATED=0
    pkg_install nodejs || true
  else
    pkg_install nodejs npm || true
  fi
  if command -v npm >/dev/null 2>&1 && node_version_ok; then
    echo "[INFO] 已安装 Node $(node -v) / npm $(npm -v)"
  else
    echo "[WARN] 自动安装的 Node.js 版本可能过旧（需 ${NODE_MIN_MAJOR}+），前端构建可能失败，请手动升级 Node.js"
  fi
}

ensure_deps() {
  command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd，无法自动安装"; exit 1; }
  detect_pkg_mgr || echo "[WARN] 未找到受支持的包管理器，将尝试直接使用已有命令"
  ensure_cmd curl curl ca-certificates || { echo "[ERROR] 需要 curl，请手动安装"; exit 1; }
  ensure_cmd tar tar || true
  ensure_cmd git git || { echo "[ERROR] 需要 git，请手动安装"; exit 1; }
  ensure_go
  ensure_node
}

# build_and_install clones the repo into a temporary directory, builds the
# frontend and the server, installs them, and finally removes the source tree.
build_and_install() {
  ensure_deps
  echo "[INFO] 拉取最新代码并本地编译: $REPO_URL ($REPO_REF)"
  BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/yals-build.XXXXXX")"
  # 无论成功或失败，结束时都删除编译源码目录
  trap 'rm -rf "$BUILD_DIR"' EXIT

  git clone --depth 1 --branch "$REPO_REF" "$REPO_URL" "$BUILD_DIR"

  echo "[INFO] 构建前端 (vite)..."
  ( cd "$BUILD_DIR/frontend" && npm ci && npm run build )

  echo "[INFO] 编译 yals_server..."
  ( cd "$BUILD_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o yals_server ./cmd/server )

  install -D -m 0755 "$BUILD_DIR/yals_server" "$SERVER_BIN"

  echo "[INFO] 部署前端到 $WEB_DIR ..."
  mkdir -p "$WEB_DIR"
  rm -rf "$WEB_DIR/assets"
  ( cd "$BUILD_DIR/web" && for item in *; do
      # 保留运营方已存在的 custom/（含自定义 config.json 等）
      if [ "$item" = "custom" ] && [ -d "$WEB_DIR/custom" ]; then
        continue
      fi
      cp -a "$item" "$WEB_DIR/"
    done )

  rm -rf "$BUILD_DIR"
  trap - EXIT
  echo "[INFO] 二进制已安装: $SERVER_BIN"
  echo "[INFO] 已删除编译源码目录"
}

write_config() {
  mkdir -p "$SERVER_DIR"

  if [ -f "$CONFIG_FILE" ]; then
    cp "$CONFIG_FILE" "$CONFIG_FILE.bak"
    echo "[INFO] 旧配置已备份为 $CONFIG_FILE.bak"
  fi

  cat > "$CONFIG_FILE" <<EOF
# YALS Server Configuration

server:
  host: "$SERVER_HOST"
  port: $SERVER_PORT
  password: "$SERVER_PASSWORD"
  log_level: "info"

database:
  path: "$SERVER_DIR/data/yals.db"
EOF

  echo "[INFO] 配置文件已生成: $CONFIG_FILE"
}

write_service() {
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=YALS Server
After=network.target

[Service]
Type=simple
ExecStart=$SERVER_BIN -c $CONFIG_FILE -w $WEB_DIR
Restart=always
RestartSec=5s
User=root
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
}

if [[ "$1" == "update" ]]; then
  echo "========== YALS SERVER 更新模式 =========="
  build_and_install
  write_service
  systemctl daemon-reload
  systemctl restart yals.service
  echo "✅ [SUCCESS] 更新完成并已重启服务"
  echo "🧠 查看运行状态: systemctl status yals.service"
  exit 0
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-host) SERVER_HOST="$2"; shift 2;;
    --server-port) SERVER_PORT="$2"; shift 2;;
    --server-password) SERVER_PASSWORD="$2"; shift 2;;
    --web-dir) WEB_DIR="$2"; shift 2;;
    --repo) REPO_URL="$2"; shift 2;;
    --ref) REPO_REF="$2"; shift 2;;
    *)
      echo "未知参数: $1"
      echo "用法示例:"
      echo "  sudo ./install_server.sh --server-host 0.0.0.0 --server-port 8080 --server-password abc123 --web-dir /etc/yals/web"
      echo "可选: --repo <git地址或本地路径> --ref <分支/标签> (默认 $REPO_URL / $REPO_REF)"
      exit 1
      ;;
  esac
done

if [[ -z "$SERVER_HOST" || -z "$SERVER_PORT" || -z "$SERVER_PASSWORD" ]]; then
  echo "[ERROR] 缺少必要参数: --server-host, --server-port, --server-password"
  exit 1
fi

echo "========== YALS SERVER 安装 / 升级开始 =========="
mkdir -p "$SERVER_DIR"
mkdir -p "$WEB_DIR"
mkdir -p "$SERVER_DIR/data"

build_and_install
write_config
write_service
systemctl daemon-reload
systemctl enable yals.service
systemctl restart yals.service

echo "✅ [SUCCESS] 安装完成"
echo "📁 配置文件路径: $CONFIG_FILE"
echo "🌐 前端目录路径: $WEB_DIR"
echo "🧠 查看运行状态: systemctl status yals.service"
echo "🪶 实时日志查看: journalctl -u yals.service -f"
