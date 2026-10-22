-- 图片与标签的关联:一张图可有多个标签,一个标签可用在多张图上。
CREATE TABLE image_tags (
    image_id TEXT NOT NULL REFERENCES images (id) ON DELETE CASCADE, -- 图片短 ID,图片删除时关联随之删除
    tag_id INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE, -- 标签
    PRIMARY KEY (image_id, tag_id)
);

CREATE INDEX idx_image_tags_tag ON image_tags (tag_id);
