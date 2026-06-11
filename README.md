# YALS NR — Yet Another Looking Glass

YALS NR is a self-hosted, distributed **network looking glass**. A central
**server** manages many **agents**; each agent runs network‑diagnostic commands
(ping, mtr, tcping, iperf3, …) on its own host and streams the output back to the
server in real time. End users pick a node and a command from a web UI;
administrators manage nodes and commands from a control panel.

- **Server** — one unified HTTPS endpoint that serves the web UI, a REST API, the
  Prometheus `/metrics` endpoint, and the gRPC service that agents connect to.
- **Agent** — connects out to the server over gRPC/TLS, authenticates with a
  UUID + token, receives its allowed command set, and executes commands on demand.
- **Frontend** — React + Vite + TypeScript, built into static assets the server
  serves.
- **Storage** — embedded SQLite (pure‑Go, no CGO) for agents and runtime settings.

---

## Table of contents

- [Architecture](#architecture)
- [Project layout](#project-layout)
- [Prerequisites](#prerequisites)
- [Quick start (full flow)](#quick-start-full-flow)
- [Build from source](#build-from-source)
- [Server configuration](#server-configuration)
- [Running the server](#running-the-server)
- [Registering and running an agent](#registering-and-running-an-agent)
- [Using the looking glass](#using-the-looking-glass)
- [Command templates and plugins](#command-templates-and-plugins)
- [One-line install (systemd)](#one-line-install-systemd)
- [HTTP API reference](#http-api-reference)
- [Observability (Prometheus + Grafana)](#observability-prometheus--grafana)
- [Security notes](#security-notes)

---

## Architecture

```
                            ┌──────────────────────────────────┐
  Browser (end user)  ─────▶│  YALS Server  (HTTPS, e.g. :8080) │
  Browser (admin)     ─────▶│   • Looking Glass UI      /        │
                            │   • Control panel         /control │
                            │   • REST API              /api/*    │
  Prometheus  ── scrape ──▶ │   • Metrics               /metrics  │
                            │   • gRPC (agent channel)           │◀─ gRPC/TLS ─ Agent #1
                            │   • SQLite (agents, settings)      │◀─ gRPC/TLS ─ Agent #2
                            └──────────────────────────────────┘◀─ gRPC/TLS ─ Agent #N
```

The server multiplexes HTTP and gRPC on a **single TLS port**: HTTP/2 requests
with `Content-Type: application/grpc` are routed to the gRPC service; everything
else goes to the web UI / REST API. Agents dial the server (the server never
dials agents), so agents work behind NAT without inbound ports.

Command execution flow:

```
end user ──POST /api/exec──▶ server ──gRPC stream──▶ agent ──runs command──┐
   ▲                                                                       │
   └──────────────── SSE stream (live output) ◀── server ◀── gRPC stream ◀─┘
```

---

## Project layout

```
cmd/server/        Server entrypoint (main.go)
cmd/agent/         Agent entrypoint (main.go)
internal/agent/    Agent manager, gRPC connection, command execution
internal/handler/  HTTP handlers, REST API, control panel, /metrics
internal/plugin/   Plugin framework + built-in agent plugins
                     (mtr, tcping, udping, speedtest, geekbench6)
internal/store/    SQLite persistence (agents, runtime settings)
internal/config/   Config structs and loaders
internal/tls/      Self-signed certificate generation
internal/proto/    Hand-written gRPC service (JSON codec)
frontend/          React + Vite + TypeScript web UI (builds into ../web)
docs/              Prometheus + Grafana integration guide and dashboard
install_server.sh  Build-from-source installer/updater for the server (systemd)
install_agent.sh   Build-from-source installer/updater for the agent (systemd)
config.yaml        Sample server configuration
```

---

## Prerequisites

To build from source you need:

- **Go 1.25+** (the module targets `go 1.25.6`)
- **Node.js 18+ and npm** (to build the frontend — server only)
- **git**

The produced binaries are statically linked (`CGO_ENABLED=0`, pure‑Go SQLite),
so the runtime hosts need nothing extra. Production deployment uses **systemd**.

---

## Quick start (full flow)

This takes you from an empty machine to a working looking glass with one node.

1. **Build the server** (frontend + binary) and the **agent** — see
   [Build from source](#build-from-source).
2. **Create `config.yaml`** with a control‑panel password — see
   [Server configuration](#server-configuration).
3. **Start the server**:
   ```bash
   ./yals_server -c config.yaml -w ./web
   ```
4. **Open the control panel** at `https://<server>:<port>/control` and log in
   with the password from `config.yaml`.
5. **Create an agent** in the control panel (name, group, commands). On save you
   get a **UUID** and **Token**, plus a ready‑to‑paste agent command.
6. **Start the agent** on the node host with those credentials:
   ```bash
   ./yals_agent -s <server-host> -p <port> -u <uuid> -t <token>
   ```
7. The node turns **online** on the looking glass at `https://<server>:<port>/`.
8. **Run a command**: pick the node, pick a command (e.g. `ping`), enter a target
   IP/domain, and click **Run** to stream live output.

> Self‑signed TLS is generated automatically on first start, so browsers and the
> agent will see an untrusted certificate unless you supply your own (see
> [Security notes](#security-notes)).

---

## Build from source

### Frontend (server only)

```bash
cd frontend
npm ci
npm run build          # outputs static assets to ../web
cd ..
```

### Server binary

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o yals_server ./cmd/server
```

### Agent binary

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o yals_agent ./cmd/agent
```

Check versions and bundled plugins:

```bash
./yals_server -version
./yals_agent  -version
```

---

## Server configuration

The server reads a YAML file (default `config.yaml`). Sample:

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  password: "your_password"          # control-panel login password
  log_level: "info"                  # debug | info | warn | error
  tls_cert_file: "./cert.pem"        # auto-generated (self-signed) if missing
  tls_key_file: "./key.pem"
  trust_proxy_headers: false         # honor X-Real-IP / X-Forwarded-For only behind a trusted proxy
  metrics_enabled: false             # expose Prometheus /metrics
  metrics_token: ""                  # optional bearer token for /metrics

database:
  path: "./data/yals.db"             # SQLite database (auto-created)
```

| Key | Meaning |
|---|---|
| `server.host` / `server.port` | Bind address and unified HTTPS/gRPC port |
| `server.password` | Password for the `/control` panel |
| `server.tls_cert_file` / `tls_key_file` | TLS cert/key; a self-signed pair is generated if absent |
| `server.trust_proxy_headers` | When `true`, derive client IP from `X-Real-IP` / `X-Forwarded-For` (only enable behind a trusted reverse proxy) |
| `server.metrics_enabled` | Enable the `/metrics` endpoint (off by default) |
| `server.metrics_token` | If set, scrapers must send `Authorization: Bearer <token>` |
| `database.path` | SQLite file path |

---

## Running the server

```bash
./yals_server -c config.yaml -w ./web
```

| Flag | Default | Description |
|---|---|---|
| `-c` | `config.yaml` | Path to the configuration file |
| `-w` | `./web` | Path to the built web frontend directory |
| `-version` | — | Print version + bundled plugins and exit |

The server listens on `host:port` for **both** the web UI / REST API and agent
gRPC connections.

---

## Registering and running an agent

Agents are defined in the control panel, not in a local file.

1. Open `https://<server>:<port>/control`, log in with `server.password`.
2. Click **New**, fill in:
   - **Agent Name**, **Group**, optional **Location / Datacenter / Test IP /
     Description**.
   - One or more **commands** (shell template or built-in plugin) — see
     [Command templates and plugins](#command-templates-and-plugins).
   - A **Token** (use *Generate* for a random one).
3. **Save**. The panel shows the agent **UUID** and **Token** and an install
   command template.
4. Run the agent on the node host:
   ```bash
   ./yals_agent -s <server-host> -p <port> -u <uuid> -t <token>
   ```

| Flag | Default | Description |
|---|---|---|
| `-s` | — | Server host/address (required) |
| `-p` | `443` | Server port (required) |
| `-u` | — | Agent UUID from the control panel (required) |
| `-t` | — | Agent token from the control panel (required) |
| `-version` | — | Print version + bundled plugins and exit |

On connect the agent performs a handshake, downloads its allowed command set, and
opens a bidirectional gRPC stream. It auto-reconnects if the connection drops.
Editing the agent in the control panel pushes a live config reload.

---

## Using the looking glass

1. Open `https://<server>:<port>/`.
2. Select a node from the **Servers** list (online nodes are selectable).
3. Choose a **command** and, if required, an **IP version** (Auto/IPv4/IPv6).
4. Enter a **target** (IP address or domain) and click **Run**.
5. Output streams live; click **Stop** to abort a running command.

Targets are validated as an IP address or domain name. Per‑IP rate limiting
applies (configurable in the control panel under *Runtime Settings*).

---

## Command templates and plugins

Each command is either a **shell template** or a **built-in plugin**.

### Shell templates

A template is a command line run on the agent host. The target is inserted via
the optional `{target}` placeholder; if the placeholder is absent the target is
appended at the end:

| Template | Target | Executed |
|---|---|---|
| `ping -c 4` | `1.1.1.1` | `ping -c 4 1.1.1.1` |
| `dig {target} +short` | `1.1.1.1` | `dig 1.1.1.1 +short` |
| `uptime` (Ignore Target on) | — | `uptime` |

Per-command options:

- **Ignore Target Input** — the command takes no target.
- **Maximum Queue** — cap on concurrent executions of this command (0 = unlimited).

### Built-in plugins

Selectable from a dropdown (no free-text typos). Bundled plugins:

| Plugin | Purpose |
|---|---|
| `mtr` | MTR route/latency trace |
| `tcping` | TCP connect latency to `host:port` |
| `udping` | UDP reachability probe |
| `speedtest` | iperf3 + HTTP download speed test (target-less) |
| `geekbench6` | Geekbench 6 single-core benchmark (target-less) |

Some plugins **force** `ignore_target` and/or `maximum_queue` (e.g. `speedtest`
ignores the target); the control panel shows those fields as plugin‑controlled.
Plugin tools (e.g. `mtr`, `iperf3`) must be installed on the agent host.

---

## One-line install (systemd)

The install scripts build from source on the target host (clone → compile →
deploy → delete the source) and set up a systemd service. They need `git`, `go`
(1.25+), and — for the server — `npm`/Node.js.

**Server:**

```bash
sudo ./install_server.sh \
  --server-host 0.0.0.0 --server-port 8080 --server-password 'your_password'
# update in place later:
sudo ./install_server.sh update
```

**Agent:**

```bash
sudo ./install_agent.sh \
  --server-host lg.example.com --server-port 443 --uuid <uuid> --token <token>
# update in place later:
sudo ./install_agent.sh update --server-host lg.example.com --server-port 443 --uuid <uuid> --token <token>
```

Optional for both: `--repo <git-url-or-local-path>` and `--ref <branch/tag>`
(or env `YALS_REPO_URL` / `YALS_REPO_REF`) to build from a specific source —
including a **local repository path**. Services:
`systemctl status yals.service` / `systemctl status yals_agent.service`.

---

## HTTP API reference

All endpoints are served over HTTPS on the configured port.

Public (looking glass):

| Method | Path | Description |
|---|---|---|
| GET | `/` | Looking Glass UI (`/control` for the panel) |
| GET | `/api/node?session_id=…` | Nodes, groups, and counts |
| POST | `/api/exec?session_id=…` | Execute a command; streams output via SSE |
| POST | `/api/stop?session_id=…` | Stop a running command |
| GET | `/metrics` | Prometheus metrics (when enabled) |

The `session_id` is generated client-side (format `session_<uuid>`); it
correlates a command with its live output and stop signal.

Control panel (require `Authorization: Bearer <token>` from `/api/control/login`):

| Method | Path | Description |
|---|---|---|
| POST | `/api/control/login` | Log in with the server password → bearer token |
| GET | `/api/control/session` | Validate the current token |
| GET / POST | `/api/control/agents` | List / create agents |
| PUT / DELETE | `/api/control/agents/{uuid}` | Update / delete an agent |
| GET / PUT | `/api/control/runtime` | Get / set runtime settings (rate limit, gRPC keepalive) |
| GET | `/api/control/plugins` | List built-in plugins and their override metadata |

---

## Observability (Prometheus + Grafana)

Enable metrics in `config.yaml` (`metrics_enabled: true`, optional
`metrics_token`), then scrape `https://<server>:<port>/metrics`. Exposed gauges
include `yals_agents_total/online/offline`, `yals_agent_up{…}`, last‑connected
and first‑seen timestamps, and command counts per agent.

A ready Prometheus scrape config, a Grafana provisioning setup, and an importable
dashboard live in [`docs/`](docs/) — see [`docs/prometheus.md`](docs/prometheus.md)
and [`docs/grafana.md`](docs/grafana.md).

---

## Security notes

- **TLS:** the server generates a self‑signed certificate if none is configured.
  For production, set `tls_cert_file` / `tls_key_file` to a trusted certificate.
  The agent currently connects without verifying the server certificate, so use a
  trusted network path or pin a certificate if that matters to you.
- **Tokens & secrets:** control sessions and agent tokens are generated with a
  CSPRNG; the control password and agent/gRPC tokens are compared in constant
  time.
- **Rate limiting:** `/api/exec` is rate‑limited per real client IP. Only enable
  `trust_proxy_headers` behind a reverse proxy you control.
- **Public surface:** the looking glass executes only admin‑defined commands, with
  targets validated as IP/domain, but it is unauthenticated by design — restrict
  network access if needed.
- **`/metrics`** is off by default and exposes agent inventory; protect it with
  `metrics_token` or the network layer.
