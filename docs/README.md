# YALS NR 文档

## 可观测性 / Prometheus + Grafana

YALS server 通过单一 `/metrics` 端点(拉取模型)聚合并暴露**所有 agent 的状态数据**。

| 文件 | 用途 |
|---|---|
| [`prometheus.md`](./prometheus.md) | 开启 metrics、配置 Prometheus 抓取、指标清单、PromQL、告警示例 |
| [`prometheus.yml`](./prometheus.yml) | 可直接使用的 Prometheus scrape 配置示例 |
| [`grafana.md`](./grafana.md) | Grafana 导入/Provisioning 指南、docker-compose 示例 |
| [`grafana-dashboard.json`](./grafana-dashboard.json) | 完整仪表板,可直接在 Grafana 导入 |
| [`grafana-provisioning-datasource.yml`](./grafana-provisioning-datasource.yml) | Grafana 数据源 provisioning 示例 |
| [`grafana-provisioning-dashboard.yml`](./grafana-provisioning-dashboard.yml) | Grafana 仪表板 provisioning 示例 |

### 快速开始

1. server `config.yaml` 设 `server.metrics_enabled: true`(可选 `metrics_token`),重启。
2. 把 [`prometheus.yml`](./prometheus.yml) 的 `yals` job 合并进你的 Prometheus 配置,改好 `targets` 与 TLS/token。
3. Grafana 导入 [`grafana-dashboard.json`](./grafana-dashboard.json),数据源选你的 Prometheus。

细节见 [`prometheus.md`](./prometheus.md) 与 [`grafana.md`](./grafana.md)。
