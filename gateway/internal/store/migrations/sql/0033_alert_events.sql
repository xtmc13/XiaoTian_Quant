-- 0033: Alertmanager 告警事件档案（alerting 闭环可观测 + fingerprint 级去重）
-- 日期: 2026-09-25
--
-- 背景: Prometheus 告警经 Alertmanager webhook 推回网关（POST /api/alerts/webhook），
--   需要持久化每个告警实例的通知状态以跨重启去重：
--     fingerprint       Alertmanager v4 载荷中的告警指纹（labelset 稳定哈希）
--     notified_firing   该 fingerprint 当前事故已推送过 firing（firing 期间不重复推送）
--     notified_resolved 该 fingerprint 当前事故已推送过 resolved（resolved 只推一次）
--   状态机：firing 到达 → notified_firing=1/notified_resolved=0；
--   resolved 到达 → notified_resolved=1；同 fingerprint 再次 firing（新事故）
--   → 复位 notified_resolved=0 重新推送 firing。
--   前端 /api/alerts/active 读 notified_firing=1 且 notified_resolved=0 的记录，
--   /api/alerts/history 读 last_seen 倒序流水。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_alert_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    fingerprint TEXT NOT NULL,
    alertname TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    labels_json TEXT NOT NULL DEFAULT '',
    annotations_json TEXT NOT NULL DEFAULT '',
    starts_at INTEGER NOT NULL DEFAULT 0,
    ends_at INTEGER NOT NULL DEFAULT 0,
    notified_firing INTEGER NOT NULL DEFAULT 0,
    notified_resolved INTEGER NOT NULL DEFAULT 0,
    first_seen INTEGER NOT NULL DEFAULT 0,
    last_seen INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_alert_events_fp ON xt_alert_events(fingerprint);
CREATE INDEX IF NOT EXISTS idx_alert_events_active ON xt_alert_events(notified_firing, notified_resolved);
CREATE INDEX IF NOT EXISTS idx_alert_events_last_seen ON xt_alert_events(last_seen);
