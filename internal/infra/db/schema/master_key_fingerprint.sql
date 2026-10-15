-- 主密钥指纹:单行(id 恒为 1),首次以某把主密钥启动时写入,此后每次启动核对;
-- 主密钥缺失或不符即拒绝启动,不会用新生成或换错的主密钥继续写入、让新旧密文混用两把密钥。
CREATE TABLE master_key_fingerprint (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- 单行守卫:只允许 id=1
    fingerprint TEXT NOT NULL, -- 由主密钥派生的指纹:HKDF-SHA256 取 16 字节,小写十六进制;不可反推主密钥
    created_at TEXT NOT NULL DEFAULT (datetime('now')) -- 写入时刻,UTC 文本
);
