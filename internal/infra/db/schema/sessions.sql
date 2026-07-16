-- 后台会话:只存 session id 的哈希,DB 泄露不等于会话可劫持;过期行由清理任务回收。
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY, -- session id 明文的 SHA256(十六进制)
    user_id TEXT NOT NULL REFERENCES users (id), -- 会话归属用户
    csrf_token TEXT NOT NULL, -- 绑定本会话的 CSRF 令牌,状态变更请求须以请求头回传
    expires_at TEXT NOT NULL, -- 到期时刻,UTC 文本,过期即拒绝并待回收
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 登录时刻,UTC 文本
);

CREATE INDEX idx_sessions_user ON sessions (user_id);

CREATE INDEX idx_sessions_expires ON sessions (expires_at);
