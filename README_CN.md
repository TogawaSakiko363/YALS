# YALS - Yet Another Looking Glass

本项目采用 [AGPLv3 许可证](LICENSE)。

*** 重要 *** 我们重构了软件的发展路径，现在将同时提供开源社区版本（YALS Community）和商业版本（YALS Master）。社区版本将不包含更高拓展性的模块等专业组件，但我们会保持为该版本提供有必要的更新。我们为Looking Glass提供解决方案、定制、维护和托管服务。我们可以为您提供更高优先级的企业级软件更新，这将极大地促进我们项目的可持续发展。我们也欢迎您为本项目提供资金支持。我们十分欢迎有使用需求的用户联系我们以获取商业版本的详情与定价。

Telegram: @AUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUU

## 项目概述

YALS（Yet Another Looking Glass）是一个现代化的基于 Web 的网络诊断工具，为分布式代理提供统一的网络命令执行界面。采用 Go 后端和 React 前端构建，提供实时命令执行、代理管理和全面的网络诊断功能。

## 主要功能

- **实时网络诊断**：实时执行 ping、mtr、traceroute 等命令，支持流式输出
- **分布式代理架构**：在多个位置部署代理，实现全面的网络测试
- **基于 Web 的界面**：使用 React 构建的现代化 UI
- **安全的 WebSocket 通信**：服务器与代理之间的实时双向通信
- **代理管理**：自动代理发现、分组和健康监控
- **命令历史**：持久化的命令历史记录，支持本地存储
- **响应式设计**：在桌面和移动设备上无缝运行
- **TLS/HTTPS 支持**：可选的 TLS 加密，确保通信安全
- **离线代理清理**：自动清理离线代理，可配置超时时间

## 系统架构

### 后端（Go）
- **WebSocket 服务器**：处理与代理和前端的实时通信
- **代理管理器**：管理代理连接、状态监控和命令路由
- **配置管理**：基于 YAML 的服务器和代理配置
- **日志系统**：结构化日志，支持可配置级别

### 前端（React + TypeScript）
- **现代 React 19**：使用 hooks 和函数式组件构建
- **TypeScript**：整个应用程序的完整类型安全
- **Tailwind CSS**：实用优先的 CSS 框架
- **Vite**：快速的构建工具和开发服务器

## 快速入门

### Server快速安装
```bash
bash <(curl -sL https://raw.githubusercontent.com/TogawaSakiko363/YALS/refs/heads/main/install_server.sh) \
  --server-host 0.0.0.0 \
  --server-port 8080 \
  --server-password "<PASSWORD>" \
  --web-dir "/etc/yals/web"
```

### Agent快速安装
```bash
bash <(curl -sL https://raw.githubusercontent.com/TogawaSakiko363/YALS/refs/heads/main/install_agent.sh) \
  --server-host lg.example.com \
  --server-port 443 \
  --server-password "<PASSWORD>" \
  --server-tls true \
  --agent-name "Node 1" \
  --agent-group "Location A" \
  --location "Earth" \
  --datacenter "DEEPDARK 1" \
  --test-ip "11.4.5.14" \
  --description "Your node info"
```

## 配置说明

### 服务器配置（config.yaml）
```yaml
server:
  host: "0.0.0.0"
  port: 443
  password: "your_secure_password"
  tls: true
  tls_cert_file: "./cert.pem"
  tls_key_file: "./key.pem"

websocket:
  ping_interval: 30
  pong_wait: 60

connection:
  timeout: 10
  keepalive: 30
  retry_interval: 15
  max_retries: 0
  delete_offline_agents: 86400
```

### 代理配置（agent.yaml）
```yaml
server:
  host: "your-server.com"
  port: 443
  password: "your_secure_password"
  tls: true

agent:
  name: "节点 1"
  group: "位置 A"
  details:
    location: "您的位置"
    datacenter: "您的数据中心"
    test_ip: "1.2.3.4"
    description: "您的节点描述"

commands:
  ping:
    template: "ping -c 4"
    description: "网络连通性测试"
  
  mtr:
    use_plugin: "mtr" # 插件功能仅限商业版本可用，接受插件定制
    description: "网络路由分析"
```

## 使用方法

1. **访问 Web 界面**：在浏览器中打开 `https://your-server.com`
2. **选择代理**：从下拉菜单中选择可用的代理
3. **选择命令**：从代理的可用命令中选择
4. **输入目标**：输入要测试的 IP 地址或主机名
5. **执行**：点击执行按钮运行命令
6. **查看结果**：实时输出显示在终端界面中

## 安全特性

- **密码认证**：所有连接都需要密码认证
- **TLS 加密**：可选的 TLS 支持，确保通信安全
- **命令白名单**：代理仅执行预配置的命令
- **输入验证**：所有用户输入都经过验证和清理

## API 文档

### WebSocket 消息

#### 代理到服务器
- `agent_status`：代理状态更新
- `command_output`：命令执行输出
- `command_complete`：命令完成通知

#### 服务器到代理
- `execute_command`：执行命令
- `get_agent_commands`：请求可用命令

#### 服务器到客户端
- `agent_status`：代理状态信息
- `commands_list`：可用命令列表
- `command_output`：实时命令输出
- `app_config`：应用程序配置

## 故障排除

### 常见问题

1. **代理连接失败**
   - 检查服务器主机和端口配置
   - 验证代理和服务器之间的密码匹配
   - 检查防火墙规则

2. **TLS 证书错误**
   - 确保证书文件存在且可读
   - 验证证书有效性和域名匹配
   - 检查文件权限

3. **命令无法执行**
   - 验证命令已在 agent.yaml 中配置
   - 查看代理日志中的错误

4. **WebSocket 连接问题**
   - 检查 WebSocket ping/pong 设置
   - 验证代理/防火墙是否允许 WebSocket 连接
   - 查看浏览器控制台中的错误

---

**如果这个项目对您有帮助，请给它一个 ⭐！**