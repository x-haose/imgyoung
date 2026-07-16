-- 上传 Token:数据 API 的凭证,只存哈希;吊销以 revoked_at 表达,行保留供审计追溯。
CREATE TABLE tokens (
    id INTEGER PRIMARY KEY, -- 内部主键(rowid 别名,自增)
    user_id TEXT NOT NULL REFERENCES users (id), -- 所属用户
    name TEXT NOT NULL, -- 具名用途(如 picgo/脚本),同一用户的有效 Token 内唯一
    token_hash TEXT NOT NULL, -- Token 明文的 SHA256(十六进制),明文不落库
    prefix TEXT NOT NULL, -- 明文前 12 字符,供列表识别,不足以还原明文
    scopes TEXT NOT NULL DEFAULT 'upload', -- 授权范围,逗号分隔;当前仅 upload
    last_used_at TEXT, -- 最近使用时刻,异步批量回写;NULL 表示从未使用
    revoked_at TEXT, -- 吊销时刻,NULL 表示有效
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 签发时刻,UTC 文本
);

CREATE UNIQUE INDEX idx_tokens_hash ON tokens (token_hash);

CREATE UNIQUE INDEX idx_tokens_user_name ON tokens (user_id, name) WHERE revoked_at IS NULL;
