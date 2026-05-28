#!/usr/bin/env python3
import json
import os
from datetime import datetime
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

try:
    from zoneinfo import ZoneInfo
except ImportError:
    ZoneInfo = None


def env(name, default=""):
    return os.environ.get(name, default).strip()


FEISHU_WEBHOOK_URL = env("FEISHU_WEBHOOK_URL")
FEISHU_KEYWORD = env("FEISHU_KEYWORD")
FEISHU_MESSAGE_TEMPLATE = env("FEISHU_MESSAGE_TEMPLATE", "card").lower()
ALERT_ENV = env("ALERT_ENV", "local")
ALERT_TZ = env("ALERT_TZ", "Asia/Shanghai")
PROMETHEUS_URL = env("PROMETHEUS_URL", "http://127.0.0.1:9090")
ALERTMANAGER_URL = env("ALERTMANAGER_URL", "http://127.0.0.1:9093")
GRAFANA_URL = env("GRAFANA_URL", "http://127.0.0.1:3001")
GRAFANA_DASHBOARD_URL = env("GRAFANA_DASHBOARD_URL")
LISTEN_HOST = env("LISTEN_HOST", "0.0.0.0")
LISTEN_PORT = int(env("LISTEN_PORT", "5001"))
HTTP_TIMEOUT = float(env("HTTP_TIMEOUT_SECONDS", "5"))


def build_text(payload):
    alerts = payload.get("alerts", [])
    if not alerts:
        return "[Alertmanager] empty alerts payload"

    first = alerts[0]
    labels = first.get("labels", {})
    annotations = first.get("annotations", {})

    status = payload.get("status", first.get("status", "unknown"))
    alertname = labels.get("alertname", "unknown")
    severity = labels.get("severity", "unknown")
    instance = labels.get("instance", "unknown")
    summary = annotations.get("summary", "")
    description = annotations.get("description", "")
    total = len(alerts)

    header = f"[{status.upper()}] {alertname}"
    if FEISHU_KEYWORD:
        header = f"{FEISHU_KEYWORD} {header}"

    lines = [
        header,
        f"severity: {severity}",
        f"instance: {instance}",
        f"alerts: {total}",
    ]

    if summary:
        lines.append(f"summary: {summary}")
    if description:
        lines.append(f"desc: {description}")

    return "\n".join(lines)


def color_from_status(status):
    normalized = (status or "").lower()
    if normalized == "firing":
        return "red"
    if normalized == "resolved":
        return "green"
    return "orange"


def severity_badge(severity):
    normalized = (severity or "").lower()
    if normalized == "critical":
        return "🔴 CRITICAL"
    if normalized == "warning":
        return "🟠 WARNING"
    if normalized == "info":
        return "🔵 INFO"
    return f"⚪ {normalized.upper() if normalized else 'UNKNOWN'}"


def status_badge(status):
    normalized = (status or "").lower()
    if normalized == "firing":
        return "🔥 FIRING"
    if normalized == "resolved":
        return "✅ RESOLVED"
    return f"ℹ️ {normalized.upper() if normalized else 'UNKNOWN'}"


def target_label(labels):
    instance = labels.get("instance", "").strip()
    if instance:
        return instance

    channel_name = labels.get("channel_name", "").strip()
    channel_id = labels.get("channel_id", "").strip()
    channel_type = labels.get("channel_type", "").strip()
    if channel_name or channel_id or channel_type:
        return f"{channel_name or '-'} (id={channel_id or '-'}, type={channel_type or '-'})"

    job = labels.get("job", "").strip()
    if job:
        return f"job:{job}"

    method = labels.get("method", "").strip()
    route = labels.get("route", "").strip()
    status_code = labels.get("status_code", "").strip()
    if method or route:
        base = f"{method or '-'} {route or '-'}".strip()
        if status_code:
            return f"{base} [{status_code}]"
        return base

    return "unknown"


def build_context_md(alert):
    labels = alert.get("labels", {})
    trigger = labels.get("trigger", "-")
    job = labels.get("job", "-")
    channel_id = labels.get("channel_id", "-")
    channel_name = labels.get("channel_name", "-")
    channel_type = labels.get("channel_type", "-")
    instance = labels.get("instance", "-")
    method = labels.get("method", "-")
    route = labels.get("route", "-")
    status_code = labels.get("status_code", "-")
    status_class = labels.get("status_class", "-")
    model = labels.get("model", "-")

    context_lines = []
    if trigger != "-":
        context_lines.append(f"- trigger: `{trigger}`")
    if job != "-":
        context_lines.append(f"- job: `{job}`")

    if channel_name != "-" or channel_id != "-" or channel_type != "-":
        context_lines.append(f"- channel: `{channel_name}` (id={channel_id}, type={channel_type})")
    if instance != "-":
        context_lines.append(f"- instance: `{instance}`")
    if method != "-":
        context_lines.append(f"- method: `{method}`")
    if route != "-":
        context_lines.append(f"- route: `{route}`")
    if status_code != "-" or status_class != "-":
        context_lines.append(f"- status: `{status_code}` ({status_class})")
    if model != "-":
        context_lines.append(f"- model: `{model}`")

    if str(channel_id) == "0" or str(channel_type).lower() == "unknown":
        context_lines.append("- hint: 选渠前失败（未命中具体渠道），常见于模型未匹配/分组无可用渠道/渠道被禁用")
    if model == "-":
        context_lines.append("- hint: 当前告警未携带模型标签（需指标侧上报 model 维度后可显示）")

    if not context_lines:
        context_lines.append("- hint: 当前告警标签较少，建议关注 NewAPIRelayHTTPErrorBurst 以获取 method/route/status 维度")

    if alert.get("generatorURL"):
        context_lines.append(f"- source: [PromQL Graph]({alert.get('generatorURL')})")

    return "\n".join(context_lines)


def parse_time(ts):
    if not ts:
        return "-"
    if str(ts).startswith("0001-01-01"):
        return "-"
    normalized = ts.replace("Z", "+00:00")
    try:
        dt = datetime.fromisoformat(normalized)
        if dt.year <= 1:
            return "-"
        if ZoneInfo and ALERT_TZ:
            dt = dt.astimezone(ZoneInfo(ALERT_TZ))
            return dt.strftime("%Y-%m-%d %H:%M:%S %Z")
        return dt.strftime("%Y-%m-%d %H:%M:%S UTC")
    except ValueError:
        return ts


def build_card(payload):
    alerts = payload.get("alerts", [])
    if not alerts:
        text = "[Alertmanager] empty alerts payload"
        return {
            "msg_type": "interactive",
            "card": {
                "header": {
                    "title": {"tag": "plain_text", "content": "Alertmanager"},
                    "template": "grey",
                },
                "elements": [{"tag": "markdown", "content": text}],
            },
        }

    first = alerts[0]
    labels = first.get("labels", {})
    status = payload.get("status", first.get("status", "unknown"))
    alertname = labels.get("alertname", "unknown")
    starts_at = parse_time(first.get("startsAt"))
    ends_at = parse_time(first.get("endsAt"))
    top_severity = severity_badge(labels.get("severity", "unknown"))
    top_status = status_badge(status)

    title = f"[{status.upper()}] {alertname}"
    if FEISHU_KEYWORD:
        title = f"{FEISHU_KEYWORD} {title}"

    detail_lines = []
    for idx, alert in enumerate(alerts[:10], 1):
        al = alert.get("labels", {})
        line = "{idx}. **{name}**\n   {severity} · `{instance}` · {astatus}".format(
            idx=idx,
            name=al.get("alertname", "unknown"),
            severity=severity_badge(al.get("severity", "unknown")),
            instance=target_label(al),
            astatus=status_badge(alert.get("status", status)),
        )
        detail_lines.append(
            line
        )

    summary = first.get("annotations", {}).get("summary", "")
    description = first.get("annotations", {}).get("description", "")
    summary_md = summary if summary else "-"
    description_md = description if description else "-"
    context_md = build_context_md(first)

    action_buttons = [
        {
            "tag": "button",
            "text": {"tag": "plain_text", "content": "Prometheus Alerts"},
            "type": "default",
            "url": f"{PROMETHEUS_URL}/alerts",
        },
        {
            "tag": "button",
            "text": {"tag": "plain_text", "content": "Alertmanager"},
            "type": "default",
            "url": f"{ALERTMANAGER_URL}/#/alerts",
        },
        {
            "tag": "button",
            "text": {"tag": "plain_text", "content": "Grafana"},
            "type": "default",
            "url": GRAFANA_URL,
        },
    ]
    if GRAFANA_DASHBOARD_URL:
        action_buttons.append(
            {
                "tag": "button",
                "text": {"tag": "plain_text", "content": "Dashboard"},
                "type": "primary",
                "url": GRAFANA_DASHBOARD_URL,
            }
        )

    return {
        "msg_type": "interactive",
        "card": {
            "config": {"wide_screen_mode": True},
            "header": {
                "title": {"tag": "plain_text", "content": title},
                "template": color_from_status(status),
            },
            "elements": [
                {
                    "tag": "div",
                    "fields": [
                        {
                            "is_short": True,
                            "text": {"tag": "lark_md", "content": f"**环境**\n{ALERT_ENV}"},
                        },
                        {
                            "is_short": True,
                            "text": {"tag": "lark_md", "content": f"**状态**\n{top_status}"},
                        },
                        {
                            "is_short": True,
                            "text": {"tag": "lark_md", "content": f"**级别**\n{top_severity}"},
                        },
                        {
                            "is_short": True,
                            "text": {"tag": "lark_md", "content": f"**告警条数**\n{len(alerts)}"},
                        },
                        {
                            "is_short": True,
                            "text": {"tag": "lark_md", "content": f"**时区**\n{ALERT_TZ}"},
                        },
                        {
                            "is_short": False,
                            "text": {"tag": "lark_md", "content": f"**首次触发**\n{starts_at}"},
                        },
                        {
                            "is_short": False,
                            "text": {"tag": "lark_md", "content": f"**预计恢复**\n{ends_at}"},
                        },
                    ],
                },
                {"tag": "hr"},
                {
                    "tag": "div",
                    "text": {
                        "tag": "lark_md",
                        "content": "**告警明细**\n" + ("\n\n".join(detail_lines) if detail_lines else "-"),
                    },
                },
                {
                    "tag": "div",
                    "text": {"tag": "lark_md", "content": f"**摘要**\n{summary_md}"},
                },
                {
                    "tag": "div",
                    "text": {"tag": "lark_md", "content": f"**描述**\n{description_md}"},
                },
                {
                    "tag": "div",
                    "text": {"tag": "lark_md", "content": f"**上下文**\n{context_md}"},
                },
                {"tag": "action", "actions": action_buttons},
                {
                    "tag": "note",
                    "elements": [
                        {
                            "tag": "plain_text",
                            "content": "new-api alert center",
                        }
                    ],
                },
            ],
        },
    }


def build_feishu_body(payload):
    if FEISHU_MESSAGE_TEMPLATE == "text":
        return {
            "msg_type": "text",
            "content": {"text": build_text(payload)},
        }
    return build_card(payload)


def send_feishu(body):
    if not FEISHU_WEBHOOK_URL:
        return 500, "FEISHU_WEBHOOK_URL is empty"

    data = json.dumps(body).encode("utf-8")
    req = Request(
        FEISHU_WEBHOOK_URL,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )

    try:
        with urlopen(req, timeout=HTTP_TIMEOUT) as resp:
            resp_body = resp.read().decode("utf-8", errors="replace")
            return resp.getcode(), resp_body
    except HTTPError as err:
        detail = err.read().decode("utf-8", errors="replace")
        return err.code, detail
    except URLError as err:
        return 502, str(err)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/healthz":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
            return

        self.send_response(404)
        self.end_headers()
        self.wfile.write(b"not found")

    def do_POST(self):
        if self.path != "/alertmanager/webhook":
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b"not found")
            return

        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)

        try:
            payload = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            self.send_response(400)
            self.end_headers()
            self.wfile.write(b"invalid json")
            return

        body = build_feishu_body(payload)
        status, detail = send_feishu(body)

        if 200 <= status < 300:
            self.send_response(200)
        else:
            self.send_response(502)
        self.end_headers()
        self.wfile.write(detail.encode("utf-8", errors="replace"))

    def log_message(self, format, *args):
        return


if __name__ == "__main__":
    server = HTTPServer((LISTEN_HOST, LISTEN_PORT), Handler)
    server.serve_forever()
