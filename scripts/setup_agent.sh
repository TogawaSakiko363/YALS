#!/bin/bash

# 脚本名称: setup_yals_user.sh
# 描述: 自动检测并安装所需依赖，创建具有最低命令授权的Linux用户
# 作者: YALS
# 创建日期: 2025-08-02

# 检查是否以root权限运行
if [ "$(id -u)" -ne 0 ]; then
    echo "错误: 此脚本需要root权限运行"
    echo "请使用 sudo ./setup_yals_user.sh 运行"
    exit 1
fi

echo "=== YALS 用户设置脚本 ==="
echo "正在检测系统类型..."

# 检测系统类型
if [ -f /etc/debian_version ]; then
    # Debian/Ubuntu
    PKG_MANAGER="apt-get"
    PKG_UPDATE="$PKG_MANAGER update"
    PKG_INSTALL="$PKG_MANAGER install -y"
    echo "检测到 Debian/Ubuntu 系统"
elif [ -f /etc/redhat-release ]; then
    # CentOS/RHEL/Fedora
    PKG_MANAGER="yum"
    PKG_UPDATE="$PKG_MANAGER makecache"
    PKG_INSTALL="$PKG_MANAGER install -y"
    echo "检测到 CentOS/RHEL/Fedora 系统"
elif [ -f /etc/arch-release ]; then
    # Arch Linux
    PKG_MANAGER="pacman"
    PKG_UPDATE="$PKG_MANAGER -Sy"
    PKG_INSTALL="$PKG_MANAGER -S --noconfirm"
    echo "检测到 Arch Linux 系统"
elif [ -f /etc/alpine-release ]; then
    # Alpine Linux
    PKG_MANAGER="apk"
    PKG_UPDATE="$PKG_MANAGER update"
    PKG_INSTALL="$PKG_MANAGER add"
    echo "检测到 Alpine Linux 系统"
else
    echo "错误: 不支持的系统类型"
    exit 1
fi

# 更新软件包列表
echo "正在更新软件包列表..."
$PKG_UPDATE

# 检查并安装curl
echo "正在检查curl..."
if ! command -v curl &> /dev/null; then
    echo "curl未安装，正在安装..."
    $PKG_INSTALL curl
    if [ $? -ne 0 ]; then
        echo "错误: 安装curl失败"
        exit 1
    fi
    echo "curl安装成功"
else
    echo "curl已安装"
fi

# 检查并安装mtr
echo "正在检查mtr..."
if ! command -v mtr &> /dev/null; then
    echo "mtr未安装，正在安装..."
    $PKG_INSTALL mtr
    if [ $? -ne 0 ]; then
        echo "错误: 安装mtr失败"
        exit 1
    fi
    echo "mtr安装成功"
else
    echo "mtr已安装"
fi

# 安装nexttrace
echo "正在安装nexttrace..."
curl nxtrace.org/nt | bash
if [ $? -ne 0 ]; then
    echo "错误: 安装nexttrace失败"
    exit 1
fi

# 为nexttrace添加必要的权限
echo "正在为nexttrace添加必要权限..."
if [ -f /usr/bin/nexttrace ]; then
    setcap cap_net_raw,cap_net_admin+eip /usr/bin/nexttrace
    echo "已为/usr/bin/nexttrace添加权限"
fi

if [ -f /usr/local/bin/nexttrace ]; then
    setcap cap_net_raw,cap_net_admin+eip /usr/local/bin/nexttrace
    echo "已为/usr/local/bin/nexttrace添加权限"
fi

echo "nexttrace安装成功"

# 生成随机密码 (16位)
RANDOM_PASSWORD=$(tr -dc 'A-Za-z0-9!@#$%^&*()' < /dev/urandom | head -c 16)

# 检查用户是否已存在
if id "yals" &>/dev/null; then
    echo "用户'yals'已存在，将重置密码和权限"
    echo "yals:$RANDOM_PASSWORD" | chpasswd
else
    # 创建用户
    echo "正在创建用户'yals'..."
    useradd -m -s /bin/bash yals
    echo "yals:$RANDOM_PASSWORD" | chpasswd
fi

# 创建sudoers文件
echo "正在配置命令授权..."
cat > /etc/sudoers.d/yals << EOF
# 允许yals用户执行特定命令而无需密码
yals ALL=(ALL) NOPASSWD: /bin/ping, /usr/bin/mtr, /usr/local/bin/nexttrace, /usr/bin/nexttrace
EOF

# 设置正确的权限
chmod 0440 /etc/sudoers.d/yals

echo "=== 设置完成 ==="
echo "用户名: yals"
echo "密码: $RANDOM_PASSWORD"
echo "授权命令: ping, mtr, nexttrace"
echo "请保存此密码信息！"
