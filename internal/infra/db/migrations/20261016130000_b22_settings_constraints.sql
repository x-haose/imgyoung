-- 存量配置按收紧后的取值范围改写,否则启动时配置校验不过
-- WebP 质量档下限为 1:编码库把 0 当作默认质量 75,存量的 0 按实际生效的值改写
UPDATE `settings` SET `process_quality_webp` = 75 WHERE `process_quality_webp` = 0;
-- base_url 须为 http/https 的站点根地址(不带路径、查询参数、片段与账号信息);不符合的存量值无法拼出可用地址,清空即按请求推导
UPDATE `settings` SET `base_url` = '' WHERE `base_url` <> '' AND NOT (
  (lower(`base_url`) LIKE 'http://%' OR lower(`base_url`) LIKE 'https://%')
  AND substr(`base_url`, instr(`base_url`, '://') + 3) <> ''
  AND NOT substr(`base_url`, instr(`base_url`, '://') + 3) GLOB '*[/?#@]*'
);
