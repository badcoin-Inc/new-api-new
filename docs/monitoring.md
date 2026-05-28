# Prometheus + Grafana + Alertmanager 监控

本项目已内置 Prometheus 指标、Grafana 看板与 Alertmanager 告警栈，覆盖：

- API QPS、耗时、错误率
- Relay 下游请求量、下游错误率、按 channel_type 的延迟
- Redis / 数据库健康状态与连接池
- 机器与容器指标（node-exporter + cAdvisor）
- 外部下游连通性探测（blackbox-exporter）

## 1. 启动

```bash
docker compose up -d
```

访问地址：

- Grafana: `http://127.0.0.1:3001`
- Prometheus: `http://127.0.0.1:9090`
- Alertmanager: `http://127.0.0.1:9093`

默认仪表盘：`New API / New API Overview`

## 2. 域名访问配置

如需通过域名访问监控服务，建议在雷池（SafeLine）或其他反向代理中配置：

- Grafana: 直接代理到 `http://grafana:3000`
- Prometheus: 直接代理到 `http://prometheus:9090`
- Alertmanager: 直接代理到 `http://alertmanager:9093`
- New API: 直接代理到 `http://new-api:3000`

注意：监控服务默认只监听 `127.0.0.1`，需要通过 Docker 网络访问。

## 3.1 Alertmanager 告警通道配置

示例配置文件：`monitoring/alertmanager/alertmanager.example.yml`

首次使用先复制：

```bash
cp monitoring/alertmanager/alertmanager.example.yml monitoring/alertmanager/alertmanager.yml
```

飞书桥接配置（将 Alertmanager 通用 JSON 转为飞书机器人消息）：

```bash
cp monitoring/feishu-bridge/.env.example monitoring/feishu-bridge/.env
```

- 默认 receiver 为 webhook：`http://feishu-bridge:5001/alertmanager/webhook`
- 在 `monitoring/feishu-bridge/.env` 填写 `FEISHU_WEBHOOK_URL`（飞书机器人地址）
- 若机器人开启关键词校验，可设置 `FEISHU_KEYWORD` 并与飞书关键词保持一致
- 可通过 `FEISHU_MESSAGE_TEMPLATE` 选择消息模板：`card`（默认，飞书消息模板）或 `text`
- 运维面板风格卡片支持配置：`ALERT_ENV`、`ALERT_TZ`、`PROMETHEUS_URL`、`ALERTMANAGER_URL`、`GRAFANA_URL`、`GRAFANA_DASHBOARD_URL`
- 修改后执行 `docker compose restart feishu-bridge alertmanager prometheus` 使配置生效

## 3.2 默认内置告警规则

规则文件：`monitoring/prometheus/rules/new-api.rules.yml`

- `NewAPIInstanceDown`：应用实例不可抓取（2 分钟）
- `DatabaseDependencyDown`：数据库依赖异常（2 分钟）
- `RedisDependencyDown`：Redis 依赖异常（2 分钟）
- `NewAPIHighHTTPErrorRate`：HTTP 错误率高于 10%（持续 10 分钟）
- `NewAPIRelayHighErrorRate`：Relay 错误率高于 15%（持续 10 分钟）
- `NewAPIChannelHighErrorCount`：单渠道 5 分钟内 5xx 错误超过 12 次（持续 2 分钟）
- `NewAPIChannelHighErrorCountAny`：单渠道 1 分钟内 4xx/5xx 错误达到 3 次（立即触发）
- `NewAPIRelayHTTPErrorBurst`：Relay 路由维度 1 分钟内某状态码错误达到 3 次（立即触发）
- `NewAPIChannelTestFailureBurst`：测试界面单渠道 1 分钟失败达到 3 次（立即触发）
- `NewAPIChannelTestFailureRateHigh`：测试界面单渠道 5 分钟失败率超过 60% 且测试量超过 3 次（持续 90 秒）
- `NewAPIChannelTestFailureRateFull`：测试界面单渠道 5 分钟失败率 100% 且测试量至少 2 次（持续 30 秒）

说明：渠道类告警仅针对已识别渠道（`channel_id != 0` 且 `channel_type != Unknown`），未获取到渠道上下文时不触发渠道告警。
- `DownstreamProbeFailed`：下游 TCP 探测失败（3 分钟）

## 4. 应用指标接口

应用新增 `GET /metrics`（Prometheus 拉取）。

- 如未设置 `METRICS_TOKEN`：默认允许内网访问
- 如设置了 `METRICS_TOKEN`：需要携带以下任一方式
  - `Authorization: Bearer <token>`
  - `X-Metrics-Token: <token>`
  - `?token=<token>`

## 5. 指标与面板含义

下面按仪表盘常见面板说明含义、来源和解读方式。

### 5.1 基础可用性与 API

- `DB Up`
  - 指标：`newapi_dependency_up{dependency="database"}`
  - 含义：数据库健康探测结果，`1=正常`，`0=异常`。

- `Redis Up`
  - 指标：`newapi_dependency_up{dependency="redis"}`
  - 含义：Redis 健康探测结果，`1=正常`，`0=异常`。

- `HTTP QPS`
  - 指标：`newapi:http_qps:rate1m`
  - 含义：应用整体每秒请求数（按 1 分钟速率计算）。

- `HTTP Error Rate`
  - 指标：`newapi:http_error_rate:ratio5m`
  - 含义：应用整体 4xx/5xx 占比（5 分钟窗口）。

- `HTTP P95`
  - 指标：`histogram_quantile(0.95, sum by (le) (rate(newapi_http_request_duration_seconds_bucket[5m])))`
  - 含义：应用整体 HTTP 延迟 95 分位（秒）。

### 5.2 Relay（真实业务流量）按渠道维度

- `Relay QPS by Channel Instance`
  - 指标：`sum by (channel_id, channel_name, channel_type) (rate(newapi_relay_requests_total[1m]))`
  - 含义：每个渠道实例的实时请求速率（每秒）。

- `Error Rate by Channel Instance`
  - 指标：基于 `newapi_relay_requests_total` 的分渠道错误率表达式（4xx/5xx ÷ 全请求）。
  - 含义：每个渠道实例在窗口内的错误比例。

- `Relay P95 by Channel Instance` / `P95 Latency by Channel Instance`
  - 指标：`newapi_relay_request_duration_seconds_bucket` 分渠道聚合后计算 P95。
  - 含义：每个渠道实例的 95 分位延迟（秒）。

说明：

- `channel_type` 是渠道类型（如 OpenAI、Anthropic）。
- `channel_id + channel_name` 是你在渠道管理里创建的具体渠道实例。
- 若出现 `[0] (Unknown)`，表示该请求未拿到渠道上下文（通常是选渠前失败或异常流程）。

### 5.3 渠道测试（独立于真实流量）

为避免污染真实业务流量，后台 `/api/channel/test/:id` 使用独立指标：

- `newapi_channel_test_total{trigger,channel_type,channel_id,channel_name,result}`
  - 含义：渠道测试次数计数。
  - `trigger`：`manual`（手动点测试）、`batch`（批量测试）、`auto`（自动巡检）。
  - `result`：`success` 或 `failure`。

- `newapi_channel_test_duration_seconds_bucket{...}`
  - 含义：渠道测试延迟分布（用于计算 P95/P99）。

常见测试面板：

- `Channel Test Success Rate (Total)`：渠道测试累计成功率。
- `Channel Test P95 Latency (15m)`：渠道测试最近 15 分钟 P95 延迟。

### 5.4 机器、容器与下游连通性

- `Machine CPU Idle`
  - 含义：CPU 空闲率，不是使用率。
  - 例如显示 `0.95`（95%）表示 CPU 使用率约 `5%`。

- `Machine Memory Used`
  - 含义：机器内存使用率，公式：`1 - MemAvailable / MemTotal`。

- `Machine Memory Available`
  - 含义：机器可用内存（GB）。

- `Downstream Reachability`
  - 指标：`probe_success`
  - 含义：blackbox 探测下游可达性，`1=可达`，`0=不可达`。

- 容器资源
  - 来源：`cadvisor`，用于查看容器级 CPU/内存等资源占用。

## 6. 下游探测目标

编辑 `monitoring/prometheus/downstream_targets.yml`，按 `host:port` 增加或删除目标。

变更后 Prometheus 会自动刷新，无需重启。

## 7. 常见现象说明

- 面板显示 `No data`
  - 优先检查 Prometheus `/targets` 是否 `up`。
  - 新启动或低频事件下，`rate()` 可能短时为空；可改 `increase()` 或增加 `or vector(0)` 兜底。

- 有请求但按渠道面板为空
  - 确认请求是否真的走了 relay 路由（如 `/v1/responses`、`/v1/chat/completions`）。
  - 后台渠道测试不计入真实 relay 面板，需看独立的 `newapi_channel_test_*` 面板。

- 错误率面板没有渠道行
  - 该渠道窗口内无错误时，若表达式未做补零，PromQL 可能不返回该渠道。
  - 可使用“按总请求补齐 0 错误”的表达式保证渠道持续可见。
