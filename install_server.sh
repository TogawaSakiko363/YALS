#!/bin/bash
# install_server.sh - YALS Server Installer / Updater

set -e

AGENT_DIR="/etc/yals"
SERVER_BIN="/usr/bin/yals_server"
SERVER_URL="https://mirror.autec.my/yals/yals_server"
CONFIG_FILE="$AGENT_DIR/config.yaml"
SERVICE_FILE="/etc/systemd/system/yals.service"
WEB_DIR="$AGENT_DIR/web"

# 检查依赖
command -v curl >/dev/null 2>&1 || { echo "[ERROR] 未安装 curl，请执行: apt install curl -y"; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd"; exit 1; }

# 处理参数
if [[ "$1" == "update" ]]; then
  echo "========== YALS SERVER 更新模式 =========="
  
  # Parse update mode parameters
  shift # Remove "update" from arguments
  UPDATE_EMAIL=""
  UPDATE_CODE=""
  UPDATE_EXPIRY=""
  
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --email) UPDATE_EMAIL="$2"; shift 2;;
      --code) UPDATE_CODE="$2"; shift 2;;
      *)
        echo "未知参数: $1"
        echo "更新模式用法: sudo ./install_server.sh update [--email <email> --code <code>]"
        exit 1
        ;;
    esac
  done

  echo "[INFO] 正在下载新版 yals_server..."
  curl -L -o "/tmp/yals_server.tmp" "$SERVER_URL"
  chmod +x "/tmp/yals_server.tmp"
  mv "/tmp/yals_server.tmp" "$SERVER_BIN"
  echo "[INFO] 二进制文件已更新"
  
  # Perform registration update if credentials provided
  if [[ -n "$UPDATE_EMAIL" && -n "$UPDATE_CODE" ]]; then
    echo "[INFO] 正在更新注册信息..."
    if [[ -n "$UPDATE_EXPIRY" ]]; then
      if ! "$SERVER_BIN" register -e "$UPDATE_EMAIL" -k "$UPDATE_CODE"; then
        echo "[ERROR] 注册更新失败"
        exit 1
      fi
    else
      if ! "$SERVER_BIN" register -e "$UPDATE_EMAIL" -k "$UPDATE_CODE"; then
        echo "[ERROR] 注册更新失败"
        exit 1
      fi
    fi
    echo "[INFO] 注册信息已更新"
  fi
  
  echo "[INFO] 更新 systemd 服务文件..."
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
  echo "✅ [SUCCESS] 更新完成并已重启服务"
  echo "🧠 查看运行状态: systemctl status yals.service"
  exit 0
fi

# ========== 正常安装模式 ==========

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-host) SERVER_HOST="$2"; shift 2;;
    --server-port) SERVER_PORT="$2"; shift 2;;
    --server-password) SERVER_PASSWORD="$2"; shift 2;;
    --web-dir) WEB_DIR="$2"; shift 2;;
    --email) REG_EMAIL="$2"; shift 2;;
    --code) REG_CODE="$2"; shift 2;;
    *)
      echo "未知参数: $1"
      echo "用法示例:"
      echo "  sudo ./install_server.sh --server-host 0.0.0.0 --server-port 8080 --server-password abc123 --web-dir /etc/yals/web --email user@example.com --code XXXXXXXXXXXXXXXX"
      exit 1
      ;;
  esac
done

if [[ -z "$SERVER_HOST" || -z "$SERVER_PORT" || -z "$SERVER_PASSWORD" ]]; then
  echo "[ERROR] 缺少必要参数: --server-host, --server-port, --server-password"
  exit 1
fi

if [[ -z "$REG_EMAIL" || -z "$REG_CODE" ]]; then
  echo "[ERROR] 缺少注册参数: --email, --code"
  exit 1
fi

echo ""
echo "========== YALS SERVER 安装 / 升级开始 =========="

download_server() {
  echo "[INFO] 下载或更新 yals_server..."
  curl -L -o "/tmp/yals_server.tmp" "$SERVER_URL"
  chmod +x "/tmp/yals_server.tmp"
  mv "/tmp/yals_server.tmp" "$SERVER_BIN"
  echo "[INFO] 下载完成: $SERVER_BIN"
}

write_config() {
  echo "[INFO] 正在生成配置文件 $CONFIG_FILE ..."

  # Create directory if it doesn't exist
  mkdir -p "$AGENT_DIR"

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

websocket:
  ping_interval: 30
  pong_wait: 60

connection:
  keepalive: 86400

rate_limit:
  enabled: true
  max_commands: 3
  time_window: 10
EOF

  echo "[INFO] 配置文件已生成"
}

setup_web() {
  mkdir -p "$WEB_DIR"
  echo "[INFO] Web 目录已创建: $WEB_DIR"
}

create_service() {
  echo "[INFO] 创建或更新 systemd 服务..."

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

  echo "[INFO] 服务已启动并设置为开机自启"
}

download_server

# Perform registration before continuing
echo "[INFO] 正在进行软件注册..."
if [[ -n "$REG_EXPIRY" ]]; then
  if ! "$SERVER_BIN" register -e "$REG_EMAIL" -k "$REG_CODE"; then
    echo "[ERROR] 注册失败，安装中止"
    exit 1
  fi
else
  if ! "$SERVER_BIN" register -e "$REG_EMAIL" -k "$REG_CODE"; then
    echo "[ERROR] 注册失败，安装中止"
    exit 1
  fi
fi

write_config
setup_web
create_service

echo ""
echo "✅ [SUCCESS] 安装完成"
echo "📁 配置文件路径: $CONFIG_FILE"
echo "🌐 前端目录路径: $WEB_DIR"
echo "🧠 查看运行状态: systemctl status yals.service"
echo "🪶 实时日志查看: journalctl -u yals.service -f"
echo "================================================="
