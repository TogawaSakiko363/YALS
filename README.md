# YALS - Yet Another Looking Glass

YALS是一个中心化设计的通过WebSocket连接控制多台agent的Looking Glass服务程序。它允许用户通过Web界面在不同的agent上执行网络诊断命令，如ping、mtr和nexttrace。

## 项目功能特点

- 通过WebSocket连接管理多台agent（已迁移自SSH架构）
- 支持执行ping、mtr和nexttrace命令
- 实时显示命令执行结果，支持流式传输
- 响应式Web界面设计
- 输入验证和安全防护
- 可配置的服务器和agent设置
- 简化部署，无需SSH配置
- 支持命令停止和状态管理

## 实战案例

  [Sharon Networks](https://lg.sharon.io)
  
  [LeiKwan Host](https://routing.leikwanhost.com/)

  [Gomami Networks](https://lg.gomami.io)

## 系统要求

### 服务器端

- 支持 Windows 和 Linux 系统

### Agent端

- 支持 Linux 系统
- 安装了ping、mtr和nexttrace命令

## 安装指南

### 部署步骤

1. **编译程序**：
```bash
# 在安装了Golang环境的Windows系统上，双击运行 `build_binaries.bat` 文件
./build_binaries.bat

# 或手动编译
go build -o yals_server ./cmd/server/main.go
go build -o yals_agent ./cmd/agent/main.go
```

2. **部署Agent**：将`yals_agent`部署到需要监控的服务器上

3. **配置服务端**：更新`config.yaml`中的agent配置

4. **启动服务**：
```bash
./yals_server -config config.yaml
```

### Agent端

1. 在目标服务器上启动Agent：

```bash
# Linux/macOS
./yals_agent -l 0.0.0.0:9527 -p your_password

# Windows
yals_agent.exe -l 0.0.0.0:9527 -p your_password
```

3. 确保防火墙允许Agent端口的访问，默认监听 `0.0.0.0:9527`

## WebSocket架构说明

### 架构变更
YALS已从SSH连接架构迁移到WebSocket客户端架构，提供更好的实时通信和流式传输支持。

#### 旧架构 (SSH) - 3.0.0版本以前
- 服务端直接通过SSH连接到远程主机
- 需要配置SSH用户名、密码或密钥
- 依赖SSH协议进行命令执行

#### 新架构 (WebSocket) 3.0.0版本及以后
- 服务端作为WebSocket服务器
- Agent客户端主动连接到服务端
- 使用密码认证
- 支持实时双向通信
- 简化部署，不需要SSH配置

### 配置格式

服务端配置 (`config.yaml`)：
```yaml
agents:
  - name: "Remote Server"
    host: "remote-server-ip:9527"
    password: "your_password"
    commands:
      - ping
      - mtr
      - nexttrace
    details:
      location: "Tokyo, JP"
      datacenter: "Your DC"
      test_ip: "remote-server-ip"
      description: "Remote monitoring server"
```

### WebSocket架构优势

1. **实时通信**：WebSocket提供低延迟的双向通信
2. **流式传输**：支持命令输出的实时流式传输
3. **简化部署**：不需要配置SSH密钥或用户账户
4. **更好的控制**：支持命令停止和状态管理
5. **防火墙友好**：使用标准HTTP/WebSocket端口

### 安全考虑

1. **密码认证**：使用强密码保护Agent连接
2. **网络隔离**：建议在内网环境中使用
3. **HTTPS/WSS**：生产环境建议使用TLS加密
4. **访问控制**：限制Agent端口的访问权限

### 故障排除

**Agent连接失败**：
- 检查网络连通性
- 验证端口是否开放
- 确认密码是否正确

**命令执行失败**：
- 检查Agent是否有执行权限
- 验证命令是否存在于系统中
- 查看Agent日志输出

### 从SSH迁移到WebSocket

1. 在目标服务器上部署Agent客户端
2. 更新服务端配置文件格式（移除SSH相关配置）
3. 重启服务端程序

配置对比：
```yaml
# 旧配置 (SSH)
agents:
  - name: "Server"
    host: "1.2.3.4"
    port: 22
    username: "user"
    password: "pass"
    key_file: "/path/to/key"

# 新配置 (WebSocket)
agents:
  - name: "Server"
    host: "1.2.3.4:9527"
    password: "pass"
```

## 配置说明

`config.yaml`文件包含以下核心配置项：

- `server`: 服务器设置（主机、端口、日志级别）
- `websocket`: WebSocket连接设置
- `agents`: Agent列表及其连接信息（包括主机地址、密码、支持命令等）
- `connection`: 连接超时和重试设置

## 前端构建说明

### 安装依赖

```bash
cd frontend
npm install
```

### 自定义网页标题和Logo

自从2.2.3版本起，可在前端目录下的 `src/custom.tsx` 实现有限的个性化Looking Glass

```
// 网页自定义配置文件
export const config = {
  // 网页标题
  pageTitle: 'Example Networks, LLC. - Looking Glass',
  
  // 右侧页脚文字内容
  footerRightText: '© 2025 Example Networks, LLC.',
  
  // 网页icon图标路径
  faviconPath: '/images/favicon.ico',
  
  // 网页左上角logo图标路径
  logoPath: '/images/Example.svg',
  
  // 网页背景颜色
  backgroundColor: '#f5f4f1'
};

// 导出类型定义，方便TypeScript类型检查
export type ConfigType = typeof config;
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

## 许可证

[MIT License](LICENSE.md)
