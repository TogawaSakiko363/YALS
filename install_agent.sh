#!/bin/bash
# install_agent.sh - YALS Agent Installer / Updater
#
# Builds from source: clones the repository, compiles the agent, installs the
# binary, then deletes the build sources.
# (Previously this downloaded a prebuilt binary from a mirror.)

set -e

AGENT_DIR="/etc/yals"
AGENT_BIN="/usr/bin/yals_agent"
REPO_URL="${YALS_REPO_URL:-https://github.com/TogawaSakiko363/YALS.git}"
REPO_REF="${YALS_REPO_REF:-main}"
SERVICE_FILE="/etc/systemd/system/yals_agent.service"

# ---- 依赖自检与自动安装 ----
GO_MIN_MAJOR=1
GO_MIN_MINOR=25
GO_FALLBACK_VERSION="go1.25.4" # 当无法获取最新版本号时使用

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
  # ensure_cmd <命令> <包名...> —— 命令缺失时尝试安装对应包
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
  major="$(echo "$v" | cut -d. -f1)"; minor="$(echo "$v" | cut -d. -f2)"
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

ensure_deps() {
  command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd，无法自动安装"; exit 1; }
  detect_pkg_mgr || echo "[WARN] 未找到受支持的包管理器，将尝试直接使用已有命令"
  ensure_cmd curl curl ca-certificates || { echo "[ERROR] 需要 curl，请手动安装"; exit 1; }
  ensure_cmd tar tar || true
  ensure_cmd git git || { echo "[ERROR] 需要 git，请手动安装"; exit 1; }
  ensure_go
}

# build_and_install clones the repo into a temporary directory, builds the
# agent, installs the binary, and finally removes the source tree.
build_and_install() {
  ensure_deps
  echo "[INFO] 拉取最新代码并本地编译: $REPO_URL ($REPO_REF)"
  BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/yals-build.XXXXXX")"
  # 无论成功或失败，结束时都删除编译源码目录
  trap 'rm -rf "$BUILD_DIR"' EXIT

  git clone --depth 1 --branch "$REPO_REF" "$REPO_URL" "$BUILD_DIR"

  echo "[INFO] 编译 yals_agent..."
  ( cd "$BUILD_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o yals_agent ./cmd/agent )

  install -D -m 0755 "$BUILD_DIR/yals_agent" "$AGENT_BIN"

  rm -rf "$BUILD_DIR"
  trap - EXIT
  echo "[INFO] 二进制已安装: $AGENT_BIN"
  echo "[INFO] 已删除编译源码目录"
}

write_service() {
  mkdir -p "$AGENT_DIR"
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=YALS Agent
After=network.target

[Service]
Type=simple
ExecStart=$AGENT_BIN -s $SERVER_HOST -p $SERVER_PORT -u $AGENT_UUID -t $AGENT_TOKEN
Restart=always
RestartSec=5s
User=root
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
}

if [[ "$1" == "update" ]]; then
  echo "========== YALS AGENT 更新模式 =========="
  shift

  # 更新模式只重建二进制并重启，复用已有的 systemd 服务（其中已含原安装参数），
  # 因此不需要 --server-host/--server-port/--uuid/--token；仅接受可选的构建源参数。
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --repo) REPO_URL="$2"; shift 2;;
      --ref) REPO_REF="$2"; shift 2;;
      *)
        echo "未知参数: $1"
        echo "更新模式用法: sudo ./install_agent.sh update [--repo <git地址或本地路径>] [--ref <分支/标签>]"
        exit 1
        ;;
    esac
  done

  if [[ ! -f "$SERVICE_FILE" ]]; then
    echo "[ERROR] 未找到现有服务 $SERVICE_FILE，请先用完整参数执行一次安装"
    exit 1
  fi

  build_and_install
  systemctl daemon-reload
  systemctl restart yals_agent.service || echo "[WARN] 无法重启服务，请检查 systemd 状态"

  echo "✅ [SUCCESS] YALS Agent 已更新并重启"
  echo "🧠 查看状态： systemctl status yals_agent.service"
  echo "🪶 查看日志： journalctl -u yals_agent -f"
  exit 0
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-host) SERVER_HOST="$2"; shift 2;;
    --server-port) SERVER_PORT="$2"; shift 2;;
    --uuid) AGENT_UUID="$2"; shift 2;;
    --token) AGENT_TOKEN="$2"; shift 2;;
    --repo) REPO_URL="$2"; shift 2;;
    --ref) REPO_REF="$2"; shift 2;;
    *)
      echo "未知参数: $1"
      echo "用法示例:"
      echo "  sudo ./install_agent.sh --server-host lg.example.com --server-port 443 --uuid <uuid> --token <token>"
      echo "可选: --repo <git地址或本地路径> --ref <分支/标签> (默认 $REPO_URL / $REPO_REF)"
      exit 1
      ;;
  esac
done

if [[ -z "$SERVER_HOST" || -z "$SERVER_PORT" || -z "$AGENT_UUID" || -z "$AGENT_TOKEN" ]]; then
  echo "[ERROR] 缺少必要参数: --server-host, --server-port, --uuid, --token"
  exit 1
fi

echo "[YALS] 正在安装 / 升级 YALS Agent..."
mkdir -p "$AGENT_DIR"

build_and_install
write_service
systemctl daemon-reload
systemctl enable yals_agent.service
systemctl restart yals_agent.service

echo "[SUCCESS] 安装完成 ✅"
echo "[INFO] 运行状态查看命令： systemctl status yals_agent.service"
echo "[INFO] 日志查看命令： journalctl -u yals_agent -f"
