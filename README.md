# YALS - Yet Another Looking Glass

This project is licensed under the [AGPLv3 License](LICENSE).

*** Important *** We have restructured the software development path and will now offer both an open-source community version (YALS Community) and a commercial version (YALS Master). The community version will not include more extensible modules or specialized components, but we will continue to provide necessary updates. We offer solutions, customization, maintenance, and hosting services for Looking Glass. We can provide you with higher-priority enterprise-level software updates, which will greatly contribute to the sustainability of our project. We also welcome your financial support for this project. Users who need our services are encouraged to contact us for details and pricing information on the commercial version.

Telegram: @AUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUU

## Overview

YALS (Yet Another Looking Glass) is a modern, web-based network diagnostic tool that provides a unified interface for executing network commands across distributed agents. Built with Go backend and React frontend, it offers real-time command execution, agent management, and comprehensive network diagnostics.

## Features

- **Real-time Network Diagnostics**: Execute commands like ping, mtr, traceroute in real-time with real-time streaming output
- **Distributed Agent Architecture**: Deploy agents across multiple locations for comprehensive network testing
- **Web-based Interface**: Modern React-based UI
- **Secure WebSocket Communication**: Real-time bidirectional communication between server and agents
- **Agent Management**: Automatic agent discovery, grouping, and health monitoring
- **Command History**: Persistent command history with local storage
- **Responsive Design**: Works seamlessly on desktop and mobile devices
- **TLS/HTTPS Support**: Secure communication with optional TLS encryption
- **Offline Agent Cleanup**: Automatic cleanup of offline agents with configurable timeout

## Architecture

### Backend (Go)
- **WebSocket Server**: Handles real-time communication with agents and frontend
- **Agent Manager**: Manages agent connections, status monitoring, and command routing
- **Configuration Management**: YAML-based configuration for server and agents
- **Logging**: Structured logging with configurable levels

### Frontend (React + TypeScript)
- **Modern React 19**: Built with hooks and functional components
- **TypeScript**: Full type safety throughout the application
- **Tailwind CSS**: Utility-first CSS framework for styling
- **Vite**: Fast build tool and development server

## Quick Start

### Server Quick Install
```bash
bash <(curl -sL https://raw.githubusercontent.com/TogawaSakiko363/YALS/refs/heads/main/install_server.sh) \
  --server-host 0.0.0.0 \
  --server-port 8080 \
  --server-password "<PASSWORD>" \
  --web-dir "/etc/yals/web"
```

### Agent Quick Install
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

## Configuration

### Server Configuration (config.yaml)
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
  keepalive: 86400
```

### Agent Configuration (agent.yaml)
```yaml
server:
  host: "your-server.com"
  port: 443
  password: "your_secure_password"
  tls: true

agent:
  name: "Node 1"
  group: "Location A"
  details:
    location: "Your Location"
    datacenter: "Your Datacenter"
    test_ip: "1.2.3.4"
    description: "Your node description"

commands:
  ping:
    template: "ping -c 4"
    description: "Network connectivity test"
  
  mtr:
    use_plugin: "mtr" # This function is for commercial versions only, we accept plugin customization
    description: "Network route analysis"
```

## Usage

1. **Access the web interface**: Open your browser to `https://your-server.com`
2. **Select an agent**: Choose from available agents in the dropdown
3. **Choose a command**: Select from available commands for the agent
4. **Enter target**: Input the IP address or hostname to test
5. **Execute**: Click execute to run the command
6. **View results**: Real-time output appears in the terminal display

## Security

- **Password Authentication**: All connections require password authentication
- **TLS Encryption**: Optional TLS support for secure communication
- **Command Whitelisting**: Agents only execute pre-configured commands
- **Input Validation**: All user inputs are validated and sanitized

## API Documentation

### WebSocket Messages

#### Agent to Server
- `agent_status`: Agent status update
- `command_output`: Command execution output
- `command_complete`: Command completion notification

#### Server to Agent
- `execute_command`: Execute a command
- `get_agent_commands`: Request available commands

#### Server to Client
- `agent_status`: Agent status information
- `commands_list`: Available commands list
- `command_output`: Real-time command output
- `app_config`: Application configuration

## Troubleshooting

### Common Issues

1. **Agent connection failed**
   - Check server host and port configuration
   - Verify password matches between agent and server
   - Check firewall rules

2. **TLS certificate errors**
   - Ensure certificate files exist and are readable
   - Verify certificate validity and domain matching
   - Check file permissions

3. **Commands not executing**
   - Verify command is configured in agent.yaml
   - Review agent logs for errors

4. **WebSocket connection issues**
   - Check WebSocket ping/pong settings
   - Verify proxy/firewall allows WebSocket connections
   - Review browser console for errors

---

**Star ⭐ this repository if you find it helpful!**