# YALS - Yet Another Looking Glass

YALS is a modern distributed Looking Glass system using WebSocket architecture for real-time communication between server and agents. The system supports agent-initiated connections with high availability and automatic reconnection, allowing users to execute network diagnostic commands on globally distributed nodes through a web interface.

## 🚀 Key Features

- **🔄 Reverse Connection**: Agents connect to server, NAT/firewall friendly
- **🔐 Secure Authentication**: Password-based WebSocket auth with TLS/WSS support
- **📡 Real-time Communication**: WebSocket bidirectional communication with streaming output
- **🎯 Auto Reconnection**: Automatic agent reconnection with connection history
- **🌐 Protocol Support**: Auto ws/wss detection, reverse proxy compatible
- **📊 Status Management**: Real-time online/offline status with connection history
- **🛡️ Command Whitelist**: Agent-side command restrictions for security
- **🎨 Responsive UI**: Modern web interface with mobile support
- **⚡ High Performance**: Concurrent command execution with intelligent sorting
- **🔧 Flexible Config**: Custom web directory, offline cleanup policies

## 🌍 Live Examples

  [Sharon Networks](https://lg.sharon.io)
  
  [LeiKwan Host](https://routing.leikwanhost.com/)

  [Gomami Networks](https://lg.gomami.io)

## ⚡ Quick Start

### Deploy Server (Linux)

```bash
bash <(curl -sL https://mirror.autec.my/yals/install_server.sh) \
  --server-host 172.18.0.1 \
  --server-port 1867 \
  --server-password "your_password"
```

### Deploy Agent

```bash
bash <(curl -sL https://mirror.autec.my/yals/install_agent.sh) \
  --server-host lg.example.com \
  --server-port 443 \
  --server-password "your_password" \
  --server-tls true \
  --agent-name "Node 1" \
  --agent-group "Location A" \
  --location "Earth" \
  --datacenter "DEEPDARK 1" \
  --test-ip "11.4.5.14" \
  --description "Your node info"
```

### Update Server/Agent

```bash
# Update Server
bash <(curl -sL https://mirror.autec.my/yals/install_server.sh) update

# Update Agent
bash <(curl -sL https://mirror.autec.my/yals/install_agent.sh) update
```

## 📋 System Requirements

### Server

- **OS**: Windows / Linux 
- **Network**: Public IP or domain with inbound connections
- **Port**: Configurable port (default 8080), reverse proxy supported

### Agent

- **OS**: Linux (Debian 12+ recommended)
- **Network**: Outbound connection to server
- **Tools**: ping, mtr, nexttrace (install before using quick start scripts)

## 🛠️ Manual Installation

### Build from Source

```bash
# Clone repository
git clone https://github.com/your-repo/yals.git
cd yals

# Windows build
./build_binaries.bat

# Linux/macOS build
go build -o yals_server ./cmd/server/main.go
go build -o yals_agent ./cmd/agent/main.go
```

### Server Setup

1. **Create config file** (`config.yaml`):
```yaml
# Application settings
app:
  version: "3.0.0-rc3"

# Server settings
server:
  host: "0.0.0.0"      # Listen address
  port: 8080           # Listen port
  password: "abc123"   # Agent connection password
  log_level: "info"

# WebSocket settings
websocket:
  ping_interval: 30    # Heartbeat interval (seconds)
  pong_wait: 60        # Heartbeat timeout (seconds)

# Connection settings
connection:
  timeout: 10
  keepalive: 30
  retry_interval: 15
  max_retries: 0
  delete_offline_agents: 86400  # Clean offline agents after 24 hours
```

2. **Start server**:
```bash
# Use default config
./yals_server

# Specify config and web directory
./yals_server -c config.yaml -w ./web
```

### Agent Setup

1. **Create config file** (`agent.yaml`):
```yaml
# Server connection
server:
  host: "lg.example.com"    # Server address
  port: 443                 # Server port
  password: "abc123"        # Connection password
  tls: true                 # Use WSS encryption (recommended)

# Agent information
agent:
  name: "Node 1"           # Agent name
  group: "Location A"      # Group name
  details:
    location: "Tokyo, JP"
    datacenter: "DC1"
    test_ip: "1.2.3.4"
    description: "Test node"

# Command whitelist
commands:
  ping:
    template: "ping -c 4"
    description: "Network connectivity test"
  mtr:
    template: "mtr -rw -c 4"
    description: "Network route and packet loss analysis"
  nexttrace:
    template: "nexttrace --nocolor --map --ipv4"
    description: "Visual route tracing"
```

2. **Start agent**:
```bash
# Start with config file
./yals_agent -c agent.yaml
```

### 🔧 Advanced Configuration

#### System Service Setup
```bash
# Create systemd service file
sudo tee /etc/systemd/system/yals-server.service > /dev/null <<EOF
[Unit]
Description=YALS Server
After=network.target

[Service]
Type=simple
User=yals
WorkingDirectory=/opt/yals
ExecStart=/opt/yals/yals_server -c /opt/yals/config.yaml -w /opt/yals/web
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Enable and start service
sudo systemctl enable yals-server
sudo systemctl start yals-server
```

## 🔐 Security Architecture

### Modern Security Design

YALS 3.0+ uses reverse connection architecture with multi-layer security mechanisms:

#### 🛡️ Core Security Features
- **🔄 Reverse Connection**: Agent connects to server, no agent port exposure
- **🔐 Password Authentication**: WebSocket password-based authentication
- **🔒 TLS Encryption**: WSS encryption support, prevents man-in-the-middle attacks
- **📝 Command Whitelist**: Agent-side strict command execution control
- **🎯 Template Execution**: Predefined command templates prevent injection
- **💓 Heartbeat Detection**: 30-second heartbeat for connection monitoring
- **🌐 Proxy Support**: Reverse proxy support with real client IP detection

#### 🔄 Architecture Evolution

| Feature | Old (SSH) | New (WebSocket) |
|---------|-----------|-----------------|
| Connection | Server → Agent | Agent → Server |
| Port Requirements | Agent needs open port | Only server needs open port |
| Firewall Friendly | ❌ Inbound rules needed | ✅ Outbound only |
| Command Control | Server-side defined | Agent-side whitelist |
| Security Risk | 🔴 Remote code execution | 🟢 Whitelist protection |
| Real-time | ❌ Batch execution | ✅ Streaming output |
| Reconnection | ❌ Manual reconnect | ✅ Auto reconnect |

### 🔒 Security Mechanisms

#### 1. Multi-layer Authentication
```
┌─────────────┐  Password Auth  ┌─────────────┐
│   Agent     │ ──────────────► │   Server    │
│             │    WSS/TLS     │             │
└─────────────┘                └─────────────┘
```

#### 2. Command Execution Security Chain
```
User Request → Server Validation → Agent Whitelist Check → Template Execution → Result Return
```

#### 3. Network Security Features
- **🔐 TLS Encryption**: WSS protocol support with encrypted data transmission
- **🌐 Proxy Friendly**: X-Real-IP and X-Forwarded-For header support
- **💓 Connection Monitoring**: Heartbeat detection with automatic disconnection
- **🚫 Access Control**: Password-based connection authentication

#### 4. Runtime Security
- **📝 Command Auditing**: All command executions are logged
- **⏱️ Timeout Protection**: Automatic command termination on timeout
- **🔄 State Isolation**: Each agent runs independently

### Security Best Practices

#### 1. Password Security
- Use strong passwords (12+ characters with mixed case, numbers, symbols)
- Rotate passwords regularly

#### 2. Network Security
- Deploy in private network environments
- Use firewall access restrictions
- Consider VPN or dedicated networks

#### 3. Command Restrictions
- Only add necessary commands to whitelist
- Regularly review command lists
- Avoid dangerous commands (rm, dd, etc.)

#### 4. Monitoring and Logging
- Monitor agent connection status
- Log command executions
- Set up anomaly alerts

## 🔧 Troubleshooting

### Common Issues

#### Agent Connection Issues
```bash
# 1. Check network connectivity
curl -I http://your-server:8080

# 2. Verify TLS configuration
openssl s_client -connect your-server:443 -servername your-domain

# 3. Check agent logs
journalctl -u yals-agent -f

# 4. Verify password configuration
grep -r "password" config.yaml agent.yaml
```

#### Command Execution Issues
```bash
# 1. Check command whitelist
./yals_agent -c agent.yaml --list-commands

# 2. Test command permissions
sudo -u yals ping -c 1 8.8.8.8

# 3. Check command paths
which ping mtr nexttrace

# 4. Verify agent status
systemctl status yals-agent
```

#### Performance Optimization
```bash
# 1. Adjust heartbeat interval
# Modify ping_interval in config.yaml

# 2. Optimize cleanup strategy  
# Set delete_offline_agents parameter

# 3. Monitor resource usage
htop
netstat -tulpn | grep yals
```

## 🚀 Advanced Features

### Reverse Proxy Configuration

#### Nginx Configuration
```nginx
server {
    listen 443 ssl http2;
    server_name lg.example.com;
    
    # SSL configuration
    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;
    
    # WebSocket proxy
    location /ws {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Host $host;
    }
    
    # Static files
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Host $host;
    }
}
```

## Frontend Build Instructions

### Install Dependencies

```bash
cd frontend
npm install
```

### Custom Web Title and Logo

Since version 2.2.3, you can customize the Looking Glass in `src/custom.tsx`:

```typescript
// Web customization config file
export const config = {
  // Page title
  pageTitle: 'Example Networks, LLC. - Looking Glass',
  
  // Footer right text
  footerRightText: '© 2025 Example Networks, LLC.',
  
  // Favicon path
  faviconPath: '/images/favicon.ico',
  
  // Logo path (top-left corner)
  logoPath: '/images/Example.svg',
  
  // Background color
  backgroundColor: '#f5f4f1'
};

// Export type definition for TypeScript
export type ConfigType = typeof config;
```

### Build Frontend

Run build command:

```bash
npm run build
```

## 🎯 Usage

### Web Interface

1. **Access Interface**: Open `http://your-server:8080` in browser
2. **Select Node**: Choose online agent from left panel
3. **Select Command**: Choose network diagnostic command
4. **Enter Target**: Input target IP address or domain
5. **Execute Command**: Click execute button to start test
6. **View Results**: Real-time command output and execution status
7. **Stop Command**: Click stop button to terminate execution anytime

### 🎨 Interface Features

- **📱 Responsive Design**: Desktop and mobile device support
- **🔄 Real-time Updates**: Agent status and command output refresh in real-time
- **📊 Smart Sorting**: Agents sorted alphabetically for consistency
- **🏷️ Group Display**: Grouped by geographic location or purpose
- **⏹️ Command Control**: Support command stop and re-execution
- **📋 Result Copy**: One-click copy of command output

## 📄 License

This project is licensed under the [MIT License](LICENSE.md).