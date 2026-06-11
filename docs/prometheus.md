# Prometheus 对接指南

YALS NR 的 server 把它所掌握的**所有 agent 的状态数据**聚合到一个 `/metrics` 端点上,采用 Prometheus 标准的**拉取(scrape)模型**:Prometheus 直接抓取 YALS server,无需 exporter 边车或 Pushgateway。

---

## 1. 在 server 启用 metrics

编辑 server 的 `config.yaml`:

```yaml
server:
  # ...
  metrics_enabled: true      # 默认 false;开启后才暴露 /metrics
  metrics_token: ""          # 可选;留空则端点无鉴权,请用网络层(防火墙)限制访问
```

- `metrics_enabled` **默认关闭**,关闭时访问 `/metrics` 返回 `404`(连端点存在性都不暴露)。
- `metrics_token` 设置后,抓取方必须发送 `Authorization: Bearer <token>`,否则返回 `401`。token 用常量时间比较。

改完后重启 server 生效。

> **传输说明**:server 始终以 TLS 提供服务(未配置证书时自动生成自签名证书),`/metrics` 与 gRPC、Web UI 复用同一个端口(`server.port`,示例配置为 `8080`)。Prometheus 必须用 `scheme: https` 抓取。

---

## 2. 配置 Prometheus

可直接使用仓库中的示例文件 [`prometheus.yml`](./prometheus.yml),核心片段:

```yaml
scrape_configs:
  - job_name: "yals"
    metrics_path: /metrics
    scheme: https
    tls_config:
      insecure_skip_verify: true     # 自签名证书;有正式证书时改用 ca_file
    authorization:                   # 仅当设置了 metrics_token 时需要
      type: Bearer
      credentials: "REPLACE_WITH_metrics_token"
    static_configs:
      - targets: ["yals.example.com:8080"]
        labels:
          instance: "yals-primary"
```

验证抓取是否成功:在 Prometheus UI 的 **Status → Targets** 中确认 `yals` job 为 `UP`,或直接手动拉取一次:

```bash
# 无 token
curl -k https://yals.example.com:8080/metrics

# 有 token
curl -k -H "Authorization: Bearer <metrics_token>" https://yals.example.com:8080/metrics
```

---

## 3. 指标清单

所有指标均为 `gauge`,每次抓取实时计算。

| 指标 | 标签 | 含义 |
|---|---|---|
| `yals_build_info` | `app`, `version` | server 构建信息,值恒为 `1` |
| `yals_agents_total` | — | 已注册 agent 总数 |
| `yals_agents_online` | — | 当前在线(已连接)agent 数 |
| `yals_agents_offline` | — | 当前离线 agent 数 |
| `yals_agent_up` | `uuid`, `name`, `group`, `location`, `datacenter` | 该 agent 是否在线:`1` 在线 / `0` 离线 |
| `yals_agent_first_seen_timestamp_seconds` | `uuid`, `name`, `group` | agent 首次注册的 Unix 时间戳(秒);未知为 `0` |
| `yals_agent_last_connected_timestamp_seconds` | `uuid`, `name`, `group` | agent 最近一次成功连接的 Unix 时间戳(秒) |
| `yals_agent_commands` | `uuid`, `name`, `group` | 该 agent 可用命令数量 |
| `yals_agent_running_commands` | `uuid`, `name`, `group` | 该 agent 当前正在运行的命令数量 |

示例输出:

```text
# HELP yals_agents_total Total number of registered agents.
# TYPE yals_agents_total gauge
yals_agents_total 3
# HELP yals_agent_up Whether the agent is currently connected (1) or not (0).
# TYPE yals_agent_up gauge
yals_agent_up{uuid="a1b2",name="tokyo",group="asia",location="JP",datacenter="ntt"} 1
yals_agent_up{uuid="c3d4",name="berlin",group="eu",location="DE",datacenter="hetzner"} 0
```

---

## 4. 常用 PromQL

```promql
# 在线率(0~1)
yals_agents_online / clamp_min(yals_agents_total, 1)

# 全部离线节点
yals_agent_up == 0

# 某分组在线节点数
sum(yals_agent_up{group="asia"})

# 节点离线时长(秒):距最近一次连接已过去多久
time() - yals_agent_last_connected_timestamp_seconds

# 全机队当前运行中的命令总数
sum(yals_agent_running_commands)

# 节点已注册时长(秒)
time() - yals_agent_first_seen_timestamp_seconds
```

---

## 5. 告警示例(可选)

把下面的规则加入 Prometheus 的 `rule_files`:

```yaml
groups:
  - name: yals
    rules:
      - alert: YalsAgentDown
        expr: yals_agent_up == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "YALS 节点 {{ $labels.name }} 离线"
          description: "节点 {{ $labels.name }}(group={{ $labels.group }}, dc={{ $labels.datacenter }})已离线超过 5 分钟。"

      - alert: YalsAllAgentsDown
        expr: yals_agents_online == 0 and yals_agents_total > 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "所有 YALS 节点离线"
          description: "已注册 {{ $value }} 个节点,但当前在线为 0。"
```

---

## 6. 可视化

参见 [`grafana.md`](./grafana.md):包含数据源/仪表板的 provisioning 示例,以及可直接导入的完整仪表板 [`grafana-dashboard.json`](./grafana-dashboard.json)。
