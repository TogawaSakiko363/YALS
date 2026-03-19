#!/bin/bash
# install_agent.sh - YALS Agent Installer / Updater

set -e

AGENT_DIR="/etc/yals"
AGENT_BIN="/usr/bin/yals_agent"
AGENT_URL="https://mirror.autec.my/yals/yals_agent"
CONFIG_FILE="$AGENT_DIR/agent.yaml"
SERVICE_FILE="/etc/systemd/system/yals_agent.service"

# ========= 更新模式 =========
if [[ "$1" == "update" ]]; then
  echo "========== YALS AGENT 更新模式 =========="

  command -v curl >/dev/null 2>&1 || { echo "[ERROR] 未安装 curl，请执行: apt install curl -y"; exit 1; }
  
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
        echo "更新模式用法: sudo ./install_agent.sh update [--email <email> --code <code>]"
        exit 1
        ;;
    esac
  done

  echo "[INFO] 正在下载最新版本 yals_agent..."
  curl -L -o "/tmp/yals_agent.tmp" "$AGENT_URL"
  chmod +x "/tmp/yals_agent.tmp"
  mv "/tmp/yals_agent.tmp" "$AGENT_BIN"

  echo "[INFO] 二进制文件更新完成: $AGENT_BIN"
  
  # Perform registration update if credentials provided
  if [[ -n "$UPDATE_EMAIL" && -n "$UPDATE_CODE" ]]; then
    echo "[INFO] 正在更新注册信息..."
    if [[ -n "$UPDATE_EXPIRY" ]]; then
      if ! "$AGENT_BIN" register -e "$UPDATE_EMAIL" -k "$UPDATE_CODE"; then
        echo "[ERROR] 注册更新失败"
        exit 1
      fi
    else
      if ! "$AGENT_BIN" register -e "$UPDATE_EMAIL" -k "$UPDATE_CODE"; then
        echo "[ERROR] 注册更新失败"
        exit 1
      fi
    fi
    echo "[INFO] 注册信息已更新"
  fi
  
  echo "[INFO] 更新 systemd 服务文件..."
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
    --email) REG_EMAIL="$2"; shift 2;;
    --code) REG_CODE="$2"; shift 2;;
    *)
      echo "未知参数: $1"
      echo "用法示例:"
      echo "  sudo ./install_agent.sh --server-host lg.example.com --server-port 443 --server-password abc123 --server-tls true"
      echo "                         --agent-name 'BGP Node 1' --agent-group 'Location A'"
      echo "                         --location 'Earth' --datacenter 'Mega Gateway'"
      echo "                         --test-ip 11.4.5.14 --description 'Provides high-quality BGP routes.'"
      echo "                         --email user@example.com --code XXXXXXXXXXXXXXXX"
      exit 1
      ;;
  esac
done

if [[ -z "$REG_EMAIL" || -z "$REG_CODE" ]]; then
  echo "[ERROR] 缺少注册参数: --email, --code"
  exit 1
fi

download_agent() {
  echo "[INFO] 下载或更新 yals_agent..."
  mkdir -p "$AGENT_DIR"
  curl -L -o "/tmp/yals_agent.tmp" "$AGENT_URL"
  chmod +x "/tmp/yals_agent.tmp"
  mv "/tmp/yals_agent.tmp" "$AGENT_BIN"
  echo "[INFO] 下载完成: $AGENT_BIN"
}

write_config() {
  echo "[INFO] 生成配置文件 $CONFIG_FILE ..."
  
  # Ensure directory exists
  mkdir -p "$AGENT_DIR"
  
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
  mtr:
    use_plugin: "mtr"
  nexttrace:
    template: "nexttrace --no-color --map --ipv4"
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
  echo "[INFO] 服务已启动并设置为开机自启"
}

echo "[YALS] 正在安装 / 升级 YALS Agent..."

download_agent

# Perform registration before continuing
echo "[INFO] 正在进行软件注册..."
if [[ -n "$REG_EXPIRY" ]]; then
  if ! "$AGENT_BIN" register -e "$REG_EMAIL" -k "$REG_CODE"; then
    echo "[ERROR] 注册失败，安装中止"
    exit 1
  fi
else
  if ! "$AGENT_BIN" register -e "$REG_EMAIL" -k "$REG_CODE"; then
    echo "[ERROR] 注册失败，安装中止"
    exit 1
  fi
fi

write_config
create_service

echo "[SUCCESS] 安装完成 ✅"
echo "[INFO] 运行状态查看命令： systemctl status yals_agent.service"
echo "[INFO] 日志查看命令： journalctl -u yals_agent -f"
