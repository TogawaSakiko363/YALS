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

command -v git >/dev/null 2>&1 || { echo "[ERROR] 未安装 git，请执行: apt install git -y"; exit 1; }
command -v go >/dev/null 2>&1 || { echo "[ERROR] 未安装 Go 工具链（需 1.25+），请先安装 Go"; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd"; exit 1; }

# build_and_install clones the repo into a temporary directory, builds the
# agent, installs the binary, and finally removes the source tree.
build_and_install() {
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
        echo "更新模式用法: sudo ./install_agent.sh update --server-host <host> --server-port <port> --uuid <uuid> --token <token>"
        exit 1
        ;;
    esac
  done

  if [[ -z "$SERVER_HOST" || -z "$SERVER_PORT" || -z "$AGENT_UUID" || -z "$AGENT_TOKEN" ]]; then
    echo "[ERROR] 更新模式缺少必要参数"
    exit 1
  fi

  build_and_install
  write_service
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
