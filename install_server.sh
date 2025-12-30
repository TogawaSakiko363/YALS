#!/bin/bash
# install_server.sh - YALS Server Installer / Updater (GitHub Release Version)

set -e

REPO="TogawaSakiko363/YALS"
ASSET_NAME="yals_server"

SERVER_DIR="/etc/yals"
SERVER_BIN="/usr/bin/yals_server"
CONFIG_FILE="$SERVER_DIR/config.yaml"
SERVICE_FILE="/etc/systemd/system/yals.service"
WEB_DIR="$SERVER_DIR/web"

command -v curl >/dev/null 2>&1 || { echo "[ERROR] 未安装 curl"; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd"; exit 1; }

# 从 GitHub Release 获取最新下载 URL
get_latest_download_url() {
  curl -s "https://api.github.com/repos/$REPO/releases/latest" \
    | grep "browser_download_url" \
    | grep "$ASSET_NAME" \
    | head -n 1 \
    | cut -d '"' -f 4
}

# ========== 更新模式 ==========
if [[ "$1" == "update" ]]; then
  echo "========== YALS SERVER 更新模式 =========="

  DOWNLOAD_URL=$(get_latest_download_url)
  if [[ -z "$DOWNLOAD_URL" ]]; then
    echo "[ERROR] 无法从 GitHub Release 获取 yals_server"
    exit 1
  fi

  echo "[INFO] 正在下载最新版 server: $DOWNLOAD_URL"
  curl -L -o "/tmp/yals_server.tmp" "$DOWNLOAD_URL"
  chmod +x "/tmp/yals_server.tmp"
  mv "/tmp/yals_server.tmp" "$SERVER_BIN"

  # 更新 systemd 服务
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

  systemctl daemon-reload
  systemctl restart yals.service

  echo "✅ 更新完成"
  exit 0
fi

# ========== 安装模式 ==========
while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-host) SERVER_HOST="$2"; shift 2;;
    --server-port) SERVER_PORT="$2"; shift 2;;
    --server-password) SERVER_PASSWORD="$2"; shift 2;;
    --web-dir) WEB_DIR="$2"; shift 2;;
    *)
      echo "未知参数: $1"
      exit 1;;
  esac
done

if [[ -z "$SERVER_HOST" || -z "$SERVER_PORT" || -z "$SERVER_PASSWORD" ]]; then
  echo "[ERROR] 缺少必要参数"
  exit 1
fi

mkdir -p "$SERVER_DIR"

DOWNLOAD_URL=$(get_latest_download_url)
if [[ -z "$DOWNLOAD_URL" ]]; then
  echo "[ERROR] 无法获取 GitHub Release 下载链接"
  exit 1
fi

echo "[INFO] 下载 yals_server..."
curl -L -o "/tmp/yals_server.tmp" "$DOWNLOAD_URL"
chmod +x "/tmp/yals_server.tmp"
mv "/tmp/yals_server.tmp" "$SERVER_BIN"

# 生成配置
cat > "$CONFIG_FILE" <<EOF
server:
  host: "$SERVER_HOST"
  port: $SERVER_PORT
  password: "$SERVER_PASSWORD"
  log_level: "info"
  tls: false
  tls_cert_file: "./cert.pem"
  tls_key_file: "./key.pem"

websocket:
  ping_interval: 30
  pong_wait: 60

connection:
  keepalive: 86400
EOF

mkdir -p "$WEB_DIR"

# systemd
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

systemctl daemon-reload
systemctl enable yals.service
systemctl restart yals.service

echo "✅ YALS Server 安装完成"
