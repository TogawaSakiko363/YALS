# YALS NR — Yet Another Looking Glass

YALS NR is a self-hosted, distributed **network looking glass**. A central
**server** manages many **agents**; each agent runs network‑diagnostic commands
(ping, mtr, tcping, iperf3, …) on its own host and streams the output back to the
server in real time. End users pick a node and a command from a web UI;
administrators manage nodes and commands from a control panel.

- **Server** — one unified HTTPS endpoint that serves the web UI, a REST API, the
  built-in monitoring pages (agent status + latency probes), and the gRPC service
  that agents connect to.
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
- [Monitoring (status + probes)](#monitoring-status--probes)
- [Security notes](#security-notes)

---

## Architecture

```
                            ┌──────────────────────────────────┐
  Browser (end user)  ─────▶│  YALS Server  (HTTPS, e.g. :8080) │
  Browser (admin)     ─────▶│   • Looking Glass UI      /        │
                            │   • Control panel         /control │
                            │   • REST API              /api/*    │
                            │   • Status / Probes       /status  │
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
internal/agent/    Agent manager, gRPC connection, command execution,
                     system-metrics collection, latency probing
internal/handler/  HTTP handlers, REST API, control panel, monitoring APIs
internal/plugin/   Plugin framework + built-in agent plugins
                     (mtr, tcping, udping, speedtest, geekbench6)
internal/probe/    targets.yaml schema, loading and hot-reload
internal/store/    SQLite persistence (agents, settings, metrics, probe results)
internal/config/   Config structs and loaders
internal/tls/      Built-in certificate
internal/proto/    Hand-written gRPC service (JSON codec)
frontend/          React + Vite + TypeScript web UI (builds into ../web)
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
6. **Start the agent** on the node host with the command shown in the control
   panel:
   ```bash
   ./yals_agent -s <server-host> -p <port> -u <uuid> -t <token>
   ```
7. The node turns **online** on the looking glass at `https://<server>:<port>/`.
8. **Run a command**: pick the node, pick a command (e.g. `ping`), enter a target
   IP/domain, and click **Run** to stream live output.

> **Agent → server TLS trust.** The server and agent ship with the **same
> built‑in self‑signed certificate**; the agent verifies the server by pinning
> it, so no certificate files or fingerprint parameters are needed. Custom
> certificates are not supported. The browser web UI will show an untrusted‑
> certificate warning — put YALS behind a TLS‑terminating reverse proxy if you
> need a browser‑trusted certificate (see [Security notes](#security-notes)).

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
  trust_proxy_headers: false         # honor X-Real-IP / X-Forwarded-For only behind a trusted proxy

database:
  path: "./data/yals.db"             # SQLite database (auto-created)
```

| Key | Meaning |
|---|---|
| `server.host` / `server.port` | Bind address and unified HTTPS/gRPC port |
| `server.password` | Password for the `/control` panel |
| `server.trust_proxy_headers` | When `true`, derive client IP from `X-Real-IP` / `X-Forwarded-For` (only enable behind a trusted reverse proxy) |
| `database.path` | SQLite file path |

Latency-probe targets are configured separately in `targets.yaml` (editable from
the control panel — see [Monitoring](#monitoring-status--probes)).

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

The agent verifies the server's TLS certificate by pinning the built‑in
certificate that both ship with — there is nothing to configure.

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
deploy → delete the source) and set up a systemd service. They auto-detect the
package manager (apt/dnf/yum/pacman/apk/zypper) and install any missing
dependencies — `git`, Go (1.25+, fetched from go.dev if absent/old), and, for the
server, Node.js (18+). systemd is required.

**Server** — pull and run the installer in one line:

```bash
curl -fsSL https://raw.githubusercontent.com/TogawaSakiko363/YALS/refs/heads/main/install_server.sh \
  | sudo bash -s -- --server-host 0.0.0.0 --server-port 8080 --server-password 'your_password'
# update in place later (from a checkout):
sudo ./install_server.sh update
```

**Agent** — same pattern (or click **Install** on the agent's row in the control
panel, which copies this command with the right uuid/token):

```bash
curl -fsSL https://raw.githubusercontent.com/TogawaSakiko363/YALS/refs/heads/main/install_agent.sh \
  | sudo bash -s -- --server-host lg.example.com --server-port 443 --uuid <uuid> --token <token>
# update in place later (rebuilds + restarts, reuses the existing service params):
sudo ./install_agent.sh update
```

Optional for both: `--repo <git-url-or-local-path>` and `--ref <branch/tag>`
(or env `YALS_REPO_URL` / `YALS_REPO_REF`) to build from a specific source —
including a **local repository path**. Services:
`systemctl status yals.service` / `systemctl status yals_agent.service`.

---

## nginx reverse proxy (public deployment)

The agent↔server link is a long-lived gRPC/HTTP2 stream, and the server serves
TLS with its self-signed certificate. To front it with nginx (terminating a real
certificate for browsers and the agents), proxy with `grpc_pass` over **`grpcs://`**
and **disable upstream verification** (the origin certificate is self-signed) —
without this the gRPC calls are answered by the web handler and fail with
`404 / Unimplemented`. Use long timeouts so the stream is not cut:

```nginx
server {
    listen 443 ssl http2;
    server_name lg.example.com;

    ssl_certificate     /etc/letsencrypt/live/lg.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/lg.example.com/privkey.pem;

    location / {
        grpc_pass grpcs://127.0.0.1:8080;

        # Origin uses YALS's self-signed certificate — do not verify it here.
        grpc_ssl_verify off;

        # Keep the long-lived agent stream alive.
        grpc_read_timeout  3600s;
        grpc_send_timeout  3600s;
    }
}
```

Note: many CDNs break long-lived gRPC streams. If you front this with a CDN,
either ensure it supports gRPC/HTTP2 end-to-end, or point agents at the origin /
a non-proxied hostname while browsers use the CDN.

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
| GET | `/api/status?session_id=…` | Latest system metrics for all agents |
| GET | `/api/probes?session_id=…&agent=&group=&window=` | Aggregated latency table |
| GET | `/api/probes/meta?session_id=…` | Agents + grouping values for the Probes UI |

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
| GET / PUT | `/api/control/targets` | Read / write the probe interval + `targets.yaml` |

---

## Monitoring (status + probes)

YALS has a built-in monitoring subsystem (no external Prometheus/Grafana needed):

- **Status** (`/status`) — a live card per agent showing CPU, memory and disk
  usage, network up/down bandwidth, and cumulative up/down traffic. Agents
  collect these (via gopsutil) and report them over the existing gRPC stream;
  the latest snapshot per agent is stored in SQLite.
- **Probes** (`/probes`) — a latency table. Each agent periodically ICMP-pings the
  targets defined in `targets.yaml` and reports latest latency, average latency
  and packet loss. Pick a vantage **agent** and a **group** (All / Location / ISP
  / Protocol) on the left, and a **time window** (1h / 6h / 12h / 24h) on the
  right. Probe results are retained for 24h.

`targets.yaml` is the single source of probe targets — each entry has one or more
IPs and a `labels` block (`name`, `location`, `isp`, `protocol`). `name` is the
unique tracking key: rename or remove a target and its old data is purged
immediately. Edit it visually from the control panel's **Monitoring** section; the
server hot-reloads it and pushes the new config to all online agents.

---

## Security notes

- **TLS (dual trust):** the agent trusts the server if **either** (1) it presents
  the **built‑in YALS self‑signed certificate** (the server serves it out of the
  box — a direct agent↔server link is encrypted/authenticated with zero config),
  **or** (2) it presents a certificate that passes **standard CA validation**
  (system roots + hostname), i.e. the server is reached through a TLS‑terminating
  reverse proxy / CDN holding a real certificate for your domain. So the same
  agent works both directly and behind a public proxy. Trade‑off on path (1): the
  built‑in certificate's private key ships with the software, so it resists a
  casual MITM but **not** an attacker who has the binary — for stronger server
  authentication use a real certificate (path 2). Agent identity is additionally
  protected by the per‑agent token.
  - **Behind a CDN/proxy:** the agent↔server link is a long‑lived gRPC/HTTP2
    stream, so the proxy must use `grpc_pass` with HTTP/2 (a plain `proxy_pass`
    or an HTTP/1.1 hop makes the server answer the gRPC call from its web handler
    → `404 / Unimplemented`). Many CDNs break long‑lived gRPC streams; the
    simplest robust setup is to **point agents at the origin** (self‑signed cert,
    pinned) while browsers use the CDN/proxy with the real certificate.
- **Tokens & secrets:** control sessions and agent tokens are generated with a
  CSPRNG; the control password and agent/gRPC tokens are compared in constant
  time.
- **Rate limiting:** `/api/exec` is rate‑limited per real client IP. Only enable
  `trust_proxy_headers` behind a reverse proxy you control.
- **Public surface:** the looking glass and the status/probes pages are
  unauthenticated by design (they execute only admin‑defined commands, with
  targets validated as IP/domain) — restrict network access if needed.
