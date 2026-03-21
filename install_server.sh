#!/bin/bash
# install_server.sh - YALS Server Installer / Updater

set -e

SERVER_DIR="/etc/yals"
SERVER_BIN="/usr/bin/yals_server"
SERVER_URL="https://mirror.autec.my/yals/yals_server"
CONFIG_FILE="$SERVER_DIR/config.yaml"
SERVICE_FILE="/etc/systemd/system/yals.service"
WEB_DIR="$SERVER_DIR/web"

command -v curl >/dev/null 2>&1 || { echo "[ERROR] 未安装 curl，请执行: apt install curl -y"; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd"; exit 1; }

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
  tls_cert_file: "$SERVER_DIR/cert.pem"
  tls_key_file: "$SERVER_DIR/key.pem"

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
  echo "[INFO] 正在下载新版 yals_server..."
  curl -L -o "/tmp/yals_server.tmp" "$SERVER_URL"
  chmod +x "/tmp/yals_server.tmp"
  mv "/tmp/yals_server.tmp" "$SERVER_BIN"

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
    *)
      echo "未知参数: $1"
      echo "用法示例:"
      echo "  sudo ./install_server.sh --server-host 0.0.0.0 --server-port 8080 --server-password abc123 --web-dir /etc/yals/web"
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

curl -L -o "/tmp/yals_server.tmp" "$SERVER_URL"
chmod +x "/tmp/yals_server.tmp"
mv "/tmp/yals_server.tmp" "$SERVER_BIN"

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
