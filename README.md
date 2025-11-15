# YALS - Yet Another Looking Glass

This project is licensed under the [MIT License](LICENSE.md), meaning you are free to use, copy, modify, merge, publish, distribute, sublicense, and even sell the software. However, we also offer customization, maintenance, and hosting services. We can provide you with higher-priority enterprise-level software updates, which will greatly contribute to the sustainable development of our project. We also welcome your financial support for this project. Please contact us for more details.

*** Important *** Version 3.0.0-final is the last version available for open source use. Starting with version 4.0.0, we will no longer provide open source code. This does not mean that YALS will no longer provide open source updates, but rather that in order to allow this project to develop better, we will still release necessary updates based on version 3.0.0-final from time to time, including useful plugin modules. We welcome users who need to use it to contact us for details and pricing of the 4.0.0+ commercial version.

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
- **Plugin System**: Extensible architecture for custom command plugins
- **Offline Agent Cleanup**: Automatic cleanup of offline agents with configurable timeout

## Architecture

### Backend (Go)
- **WebSocket Server**: Handles real-time communication with agents and frontend
- **Agent Manager**: Manages agent connections, status monitoring, and command routing
- **Plugin System**: Extensible command execution through plugins
- **Configuration Management**: YAML-based configuration for server and agents
- **Logging**: Structured logging with configurable levels

### Frontend (React + TypeScript)
- **Modern React 18**: Built with hooks and functional components
- **TypeScript**: Full type safety throughout the application
- **Tailwind CSS**: Utility-first CSS framework for styling
- **Vite**: Fast build tool and development server

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
  timeout: 10
  keepalive: 30
  retry_interval: 15
  max_retries: 0
  delete_offline_agents: 86400
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
    use_plugin: "mtr"
    description: "Network route analysis"
```

## Usage

1. **Access the web interface**: Open your browser to `https://your-server.com`
2. **Select an agent**: Choose from available agents in the dropdown
3. **Choose a command**: Select from available commands for the agent
4. **Enter target**: Input the IP address or hostname to test
5. **Execute**: Click execute to run the command
6. **View results**: Real-time output appears in the terminal display

## Command Plugins

YALS supports custom command plugins. Place your plugin executables in the appropriate plugin directory:
- Server plugins: `./internal/plugin/server/`
- Agent plugins: `./internal/plugin/agent/`

Plugins should accept command-line arguments and output results to stdout/stderr.

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
   - Check command plugin exists and is executable
   - Review agent logs for errors

4. **WebSocket connection issues**
   - Check WebSocket ping/pong settings
   - Verify proxy/firewall allows WebSocket connections
   - Review browser console for errors

---

**Star ⭐ this repository if you find it helpful!**