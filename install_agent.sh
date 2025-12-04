#!/bin/bash
# install_agent.sh - YALS Agent Installer / Updater (GitHub Release Version)

set -e

REPO="TogawaSakiko363/YALS"
ASSET_NAME="yals_agent"

AGENT_DIR="/etc/yals"
AGENT_BIN="/usr/bin/yals_agent"
CONFIG_FILE="$AGENT_DIR/agent.yaml"
SERVICE_FILE="/etc/systemd/system/yals_agent.service"

command -v curl >/dev/null 2>&1 || { echo "[ERROR] 未安装 curl"; exit 1; }

# 从 GitHub Release 获取最新下载 URL
get_latest_download_url() {
  curl -s "https://api.github.com/repos/$REPO/releases/latest" \
    | grep "browser_download_url" \
    | grep "$ASSET_NAME" \
    | head -n 1 \
    | cut -d '"' -f 4
}

# ========= 更新模式 ==========
if [[ "$1" == "update" ]]; then
  echo "========== YALS AGENT 更新模式 =========="

  DOWNLOAD_URL=$(get_latest_download_url)
  if [[ -z "$DOWNLOAD_URL" ]]; then
    echo "[ERROR] 无法从 GitHub Release 获取 yals_agent"
    exit 1
  fi

  echo "[INFO] 下载最新版 agent: $DOWNLOAD_URL"
  curl -L -o "/tmp/yals_agent.tmp" "$DOWNLOAD_URL"
  chmod +x "/tmp/yals_agent.tmp"
  mv "/tmp/yals_agent.tmp" "$AGENT_BIN"

  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=YALS Agent
After=network.target

[Service]
Type=simple
ExecStart=$AGENT_BIN -c $CONFIG_FILE
Restart=always
RestartSec=5s
User=root
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl restart yals_agent.service

  echo "✅ Agent 更新完成"
  exit 0
fi

# ========= 安装模式 ==========
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
      echo "未知参数: $1"; exit 1;;
  esac
done

mkdir -p "$AGENT_DIR"

DOWNLOAD_URL=$(get_latest_download_url)
if [[ -z "$DOWNLOAD_URL" ]]; then
  echo "[ERROR] 无法获取 GitHub Release 下载链接"
  exit 1
fi

echo "[INFO] 下载 yals_agent..."
curl -L -o "/tmp/yals_agent.tmp" "$DOWNLOAD_URL"
chmod +x "/tmp/yals_agent.tmp"
mv "/tmp/yals_agent.tmp" "$AGENT_BIN"

# 生成配置
cat > "$CONFIG_FILE" <<EOF
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
    description: "Network connectivity test"

  mtr:
    template: "mtr -rw -c 4"
    description: "Network route and packet loss analysis"

  nexttrace:
    template: "nexttrace --no-color --map --ipv4"
    description: "Visual route tracing"
EOF

# systemd
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=YALS Agent
After=network.target

[Service]
Type=simple
ExecStart=$AGENT_BIN -c $CONFIG_FILE
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

echo "✅ YALS Agent 安装完成"
