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

command -v git >/dev/null 2>&1 || { echo "[ERROR] 未安装 git，请执行: apt install git -y"; exit 1; }
command -v go >/dev/null 2>&1 || { echo "[ERROR] 未安装 Go 工具链（需 1.25+），请先安装 Go"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "[ERROR] 未安装 npm/Node.js（前端构建所需），请先安装 Node.js"; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "[ERROR] 当前系统不支持 systemd"; exit 1; }

# build_and_install clones the repo into a temporary directory, builds the
# frontend and the server, installs them, and finally removes the source tree.
build_and_install() {
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
