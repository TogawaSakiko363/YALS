# Grafana 对接指南

本目录提供一个**可直接导入**的 Grafana 仪表板,用于可视化 YALS NR 通过 Prometheus 暴露的全机队 agent 状态。

数据链路:

```
YALS server (/metrics) ──scrape──> Prometheus ──query──> Grafana 仪表板
```

> 前置条件:先按 [`prometheus.md`](./prometheus.md) 开启 `metrics_enabled` 并让 Prometheus 成功抓取 YALS。Grafana 查询的是 **Prometheus**,不是 YALS server。

---

## 方式一:UI 手动导入(最快)

1. Grafana 左侧 **Dashboards → New → Import**。
2. 上传 [`grafana-dashboard.json`](./grafana-dashboard.json),或粘贴其内容。
3. 在导入向导里把 `DS_PROMETHEUS` 选择为你抓取 YALS 的 Prometheus 数据源。
4. 点击 **Import**。

仪表板自带两个下拉变量 **分组(group)** 与 **节点(agent)**,可按 `group` / `name` 过滤;默认 `All`。

---

## 方式二:Provisioning 自动部署(适合容器/IaC)

仓库提供两个示例调用文件:

| 文件 | 放置路径 |
|---|---|
| [`grafana-provisioning-datasource.yml`](./grafana-provisioning-datasource.yml) | `/etc/grafana/provisioning/datasources/yals.yml` |
| [`grafana-provisioning-dashboard.yml`](./grafana-provisioning-dashboard.yml) | `/etc/grafana/provisioning/dashboards/yals.yml` |

步骤:

1. 部署数据源 provisioning(指向**你的 Prometheus**,默认 UID `prometheus-yals`)。
2. 部署仪表板 provider,并把 `grafana-dashboard.json` 复制到 provider 的 `path` 目录(示例为 `/var/lib/grafana/dashboards/yals`)。
3. 重启 Grafana。

### docker-compose 示例

```yaml
services:
  grafana:
    image: grafana/grafana:11.1.0
    ports:
      - "3000:3000"
    volumes:
      - ./docs/grafana-provisioning-datasource.yml:/etc/grafana/provisioning/datasources/yals.yml:ro
      - ./docs/grafana-provisioning-dashboard.yml:/etc/grafana/provisioning/dashboards/yals.yml:ro
      - ./docs/grafana-dashboard.json:/var/lib/grafana/dashboards/yals/yals.json:ro

  prometheus:
    image: prom/prometheus:v2.53.0
    ports:
      - "9090:9090"
    volumes:
      - ./docs/prometheus.yml:/etc/prometheus/prometheus.yml:ro
```

> 自动部署时,`grafana-dashboard.json` 里的 `${DS_PROMETHEUS}` 需解析到实际数据源 UID。若 Grafana 提示数据源未解析,把 JSON 中 `${DS_PROMETHEUS}` 全部替换为数据源 UID(如 `prometheus-yals`),或在仪表板 provider 中按 [`grafana-provisioning-dashboard.yml`](./grafana-provisioning-dashboard.yml) 注释里的 `datasources:` 映射配置。

---

## 仪表板内容

| 面板 | 类型 | 说明 |
|---|---|---|
| 节点总数 / 在线 / 离线 | Stat | 来自 `yals_agents_total/online/offline`(全机队,不受变量过滤) |
| 在线率 | Gauge | `yals_agents_online / clamp_min(yals_agents_total, 1)` |
| 运行中命令(合计) | Stat | `sum(yals_agent_running_commands)`,受变量过滤 |
| Server 版本 | Stat | 取 `yals_build_info` 的 `version` 标签 |
| 节点在线趋势 | Time series | 在线 / 离线 / 总数随时间变化 |
| 各节点在线状态 | State timeline | 每个 `yals_agent_up` 的在线/离线时段 |
| 节点清单 | Table | 名称、分组、位置、机房、在线状态(色块) |
| 节点运行与连接 | Table | 运行中命令、可用命令、距最近一次连接的秒数 |

---

## 故障排查

- **面板 "No data"**:先确认 Prometheus 的 `yals` target 为 `UP`(Prometheus → Status → Targets),并在 Grafana **Explore** 里手动查询 `yals_agents_total` 验证数据源连通。
- **变量下拉为空**:说明当前没有任何 `yals_agent_up` 序列——通常是 YALS 还没有注册任何 agent,或抓取尚未成功。
- **"Server 版本" 显示为 1 而非版本号**:确认你的 Grafana 版本支持 `${__field.labels.version}` 显示名(Grafana 9+);否则用 Explore 直接查看 `yals_build_info` 的 `version` 标签。
