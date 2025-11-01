#!/bin/bash
# install_agent.sh - YALS Agent Installer / Updater

set -e

AGENT_DIR="/etc/yals"
AGENT_BIN="$AGENT_DIR/yals_agent"
AGENT_URL="https://mirror.autec.my/yals/linux_amd64/yals_agent"
CONFIG_FILE="$AGENT_DIR/agent.yaml"
SERVICE_FILE="/etc/systemd/system/yals_agent.service"

# ========= 更新模式 =========
if [[ "$1" == "update" ]]; then
  echo "========== YALS AGENT 更新模式 =========="

  command -v curl >/dev/null 2>&1 || { echo "[ERROR] 未安装 curl，请执行: apt install curl -y"; exit 1; }

  echo "[INFO] 正在下载最新版本 yals_agent..."
  mkdir -p "$AGENT_DIR"
  curl -L -o "$AGENT_BIN.tmp" "$AGENT_URL"
  chmod +x "$AGENT_BIN.tmp"
  mv "$AGENT_BIN.tmp" "$AGENT_BIN"

  echo "[INFO] 二进制文件更新完成: $AGENT_BIN"
  systemctl restart yals_agent.service || echo "[WARN] 无法重启服务，请检查 systemd 状态"

  echo "✅ [SUCCESS] YALS Agent 已更新并重启"
  echo "🧠 查看状态： systemctl status yals_agent.service"
  echo "🪶 查看日志： journalctl -u yals_agent -f"
  exit 0
fi

# ========= 正常安装模式 =========

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-host) SERVER_HOST="$2"; shift 2;;
    --server-port) SERVER_PORT="$2"; shift 2;;
    --server-password) SERVER_PASSWORD="$2"; shift 2;;
    --server-tls) SERVER_TLS="$2"; shift 2;;
    --agent-name) AGENT_NAME="$2"; shift 2;;
    --agent-group) AGENT_GROUP="$2"; shift 2;;
    --location) DETAIL_LOCATION="$2"; shift 2;;
    --datacenter) DETAIL_DATACENTER="$2"; shift 2;;
    --test-ip) DETAIL_TEST_IP="$2"; shift 2;;
    --description) DETAIL_DESC="$2"; shift 2;;
    *)
      echo "未知参数: $1"
      echo "用法示例:"
      echo "  sudo ./install_agent.sh --server-host lg.example.com --server-port 443 --server-password abc123 --server-tls true"
      echo "                         --agent-name 'BGP Node 1' --agent-group 'Location A'"
      echo "                         --location 'Earth' --datacenter 'Mega Gateway'"
      echo "                         --test-ip 11.4.5.14 --description 'Provides high-quality BGP routes.'"
      exit 1
      ;;
  esac
done

download_agent() {
  echo "[INFO] 下载或更新 yals_agent..."
  mkdir -p "$AGENT_DIR"
  curl -L -o "$AGENT_BIN.tmp" "$AGENT_URL"
  chmod +x "$AGENT_BIN.tmp"
  mv "$AGENT_BIN.tmp" "$AGENT_BIN"
  echo "[INFO] 下载完成: $AGENT_BIN"
}

write_config() {
  echo "[INFO] 生成配置文件 $CONFIG_FILE ..."
  cat > "$CONFIG_FILE" <<EOF
# Agent Configuration File

server:
  host: "$SERVER_HOST"
  port: $SERVER_PORT
  password: "$SERVER_PASSWORD"
  tls: $SERVER_TLS

agent:
  name: "$AGENT_NAME"
  group: "$AGENT_GROUP"
  details:
    location: "$DETAIL_LOCATION"
    datacenter: "$DETAIL_DATACENTER"
    test_ip: "$DETAIL_TEST_IP"
    description: "$DETAIL_DESC"

commands:
  ping:
    template: "ping -c 4"
    description: "网络连通性测试"
  mtr:
    template: "mtr -rw -c 4"
    description: "网络路由和丢包分析"
  nexttrace:
    template: "nexttrace --nocolor --map --ipv4"
    description: "可视化路由跟踪"
EOF
  echo "[INFO] 配置文件生成完成"
}

create_service() {
  echo "[INFO] 创建或更新 systemd 服务..."
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=YALS Agent
After=network.target

[Service]
Type=simple
ExecStart=$AGENT_BIN -config $CONFIG_FILE
Restart=always
RestartSec=5s
User=root
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable yals_agent.service
  systemctl restart yals_agent.service
  echo "[INFO] 服务已启动并设置为开机自启"
}

echo "[YALS] 正在安装 / 升级 YALS Agent..."

download_agent
write_config
create_service

echo "[SUCCESS] 安装完成 ✅"
echo "[INFO] 运行状态查看命令： systemctl status yals_agent.service"
echo "[INFO] 日志查看命令： journalctl -u yals_agent -f"
