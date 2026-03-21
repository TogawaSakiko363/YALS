#!/bin/bash
# install_agent.sh - YALS Agent Installer / Updater

set -e

AGENT_DIR="/etc/yals"
AGENT_BIN="/usr/bin/yals_agent"
AGENT_URL="https://mirror.autec.my/yals/yals_agent"
SERVICE_FILE="/etc/systemd/system/yals_agent.service"

command -v curl >/dev/null 2>&1 || { echo "[ERROR] 未安装 curl，请执行: apt install curl -y"; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd"; exit 1; }

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

  echo "[INFO] 正在下载最新版本 yals_agent..."
  curl -L -o "/tmp/yals_agent.tmp" "$AGENT_URL"
  chmod +x "/tmp/yals_agent.tmp"
  mv "/tmp/yals_agent.tmp" "$AGENT_BIN"

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
    *)
      echo "未知参数: $1"
      echo "用法示例:"
      echo "  sudo ./install_agent.sh --server-host lg.example.com --server-port 443 --uuid <uuid> --token <token>"
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
curl -L -o "/tmp/yals_agent.tmp" "$AGENT_URL"
chmod +x "/tmp/yals_agent.tmp"
mv "/tmp/yals_agent.tmp" "$AGENT_BIN"

write_service
systemctl daemon-reload
systemctl enable yals_agent.service
systemctl restart yals_agent.service

echo "[SUCCESS] 安装完成 ✅"
echo "[INFO] 运行状态查看命令： systemctl status yals_agent.service"
echo "[INFO] 日志查看命令： journalctl -u yals_agent -f"
