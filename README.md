# YALS - Yet Another Looking Glass

YALS是一个通过SSH连接控制多台agent的Looking Glass服务程序。它允许用户通过Web界面在不同的agent上执行网络诊断命令，如ping、mtr和nexttrace。

### 🚀 Sponsored by SharonNetworks

本项目的构建与发布环境由 SharonNetworks 提供支持 —— 专注亚太顶级回国优化线路，高带宽、低延迟直连中国大陆，内置强大高防 DDoS 清洗能力。

SharonNetworks 为您的业务起飞保驾护航！

#### ✨ 服务优势

* 亚太三网回程优化直连中国大陆，下载快到飞起
* 超大带宽 + 抗攻击清洗服务，保障业务安全稳定
* 多节点覆盖（香港、新加坡、日本、台湾、韩国）
* 高防护力、高速网络；港/日/新 CDN 即将上线

想体验同款构建环境？欢迎 [访问 Sharon 官网](https://sharon.io) 或 [加入 Telegram 群组](https://t.me/SharonNetwork) 了解更多并申请赞助。

## 项目功能特点

- 通过SSH连接管理多台agent
- 支持执行ping、mtr和nexttrace命令
- 实时显示命令执行结果
- 响应式Web界面设计
- WebSocket实时通信
- 输入验证和安全防护
- 可配置的服务器和agent设置

## 系统要求

### 服务器端

- Go 1.23+
- 支持WebSocket的现代浏览器

### Agent端

- Linux系统
- 支持SSH连接
- 安装了ping、mtr和nexttrace命令

## 安装指南

### 服务器端

1. 克隆仓库：

```bash
git clone https://github.com/yourusername/yals.git
cd yals
```

2. 安装依赖：

```bash
go mod tidy
```

3. 编译程序：

```bash
go build -o yals ./cmd/server
```

4. 配置`config.yaml`文件：

```yaml
# 根据你的环境修改配置
```

5. 运行服务器：

```bash
./yals -config config.yaml
```

### Agent端

1. 将`scripts/setup_agent.sh`脚本复制到agent服务器上

2. 执行脚本（需要root权限）：

```bash
chmod +x setup_agent.sh
sudo ./setup_agent.sh
```

3. 记录生成的用户名和密码，并更新服务器的`config.yaml`文件

## 配置说明

`config.yaml`文件包含以下核心配置项：

- `server`: 服务器设置（主机、端口、日志级别）
- `websocket`: WebSocket连接设置
- `agents`: Agent列表及其连接信息
- `connection`: 连接超时和重试设置


## 前端构建说明

### 开发环境要求

- Node.js 16+
- npm 8+

### 安装依赖

```bash
cd frontend
npm install
```

### 构建前端

执行构建命令：

```bash
npm run build
```

## 使用方法

1. 启动服务器后，在浏览器中访问`http://your-server-ip:8080`

2. 在左侧选择一个可用的agent

3. 选择要执行的命令（ping、mtr或nexttrace）

4. 输入目标IP地址或域名

5. 点击"执行"按钮执行命令

6. 查看下方面板中的命令输出结果

## 安全注意事项

- 所有用户输入都经过验证，只允许IP地址和域名
- Agent用户权限受限，只能执行特定命令
- 使用SSH密钥认证而非密码可提高安全性
- 建议在生产环境中使用HTTPS

## 许可证

[MIT License](LICENSE.md)
