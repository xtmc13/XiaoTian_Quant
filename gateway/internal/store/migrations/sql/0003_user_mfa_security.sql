-- 0003: 用户安全加固 —— MFA/TOTP、备用码、强制改密、最近登录 IP
-- 日期: 2026-09-19
--
-- 背景: 对标 QuantDinger mfa_service.py 做安全补齐（A3.1/A3.4）。
--   - totp_secret:    TOTP 密钥（base32，明文—— authenticator 需要原文下发，
--                     泄露影响与密码哈希同级，故仍按密码哈希的防护级别对待）；
--   - totp_enabled:   MFA 是否已启用（setup 后需 enable 才置 1）；
--   - mfa_backup_codes: 一次性备用码 JSON 数组，加密存储（enc: 前缀，
--                     密钥来源 XIAOTIAN_CONFIG_KEY，未配置时退化为明文，与
--                     config.yaml 密钥字段的既有行为一致）；
--   - must_change_password: 首次登录/重置后强制改密标记（A3.4）；
--   - last_login_ip:  最近登录 IP，新 IP 且未开 MFA 时发通知提醒（风险登录）。
-- token_version 列已存在于 xt_users（schema V1 内联 DDL），本迁移不重复添加。

ALTER TABLE xt_users ADD COLUMN totp_secret TEXT DEFAULT '';
ALTER TABLE xt_users ADD COLUMN totp_enabled INTEGER DEFAULT 0;
ALTER TABLE xt_users ADD COLUMN mfa_backup_codes TEXT DEFAULT '';
ALTER TABLE xt_users ADD COLUMN must_change_password INTEGER DEFAULT 0;
ALTER TABLE xt_users ADD COLUMN last_login_ip TEXT DEFAULT '';
