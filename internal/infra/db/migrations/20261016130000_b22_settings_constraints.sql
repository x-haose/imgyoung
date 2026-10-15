-- 存量配置按收紧后的取值范围改写,否则启动时配置校验不过
-- WebP 质量档下限为 1:编码库把 0 当作默认质量 75,存量的 0 按实际生效的值改写
UPDATE `settings` SET `process_quality_webp` = 75 WHERE `process_quality_webp` = 0;
-- base_url 只保留 http/https 加主机名(字母数字开头,只含字母数字 . -)与可选数字端口的站点根地址,其余清空即按请求推导;
-- 规则比启动时的校验严,保留下来的值都能通过校验
WITH `parts` AS (
  SELECT `id`, lower(substr(`base_url`, 1, instr(`base_url`, '://') - 1)) AS `scheme`,
         substr(`base_url`, instr(`base_url`, '://') + 3) AS `authority`
  FROM `settings` WHERE `base_url` <> ''
), `split` AS (
  SELECT `id`, `scheme`,
         CASE WHEN instr(`authority`, ':') > 0 THEN substr(`authority`, 1, instr(`authority`, ':') - 1) ELSE `authority` END AS `host`,
         CASE WHEN instr(`authority`, ':') > 0 THEN substr(`authority`, instr(`authority`, ':') + 1) END AS `port`
  FROM `parts`
)
UPDATE `settings` SET `base_url` = '' WHERE `id` IN (
  SELECT `id` FROM `split`
  WHERE `scheme` NOT IN ('http', 'https')
     OR NOT `host` GLOB '[A-Za-z0-9]*'
     OR `host` GLOB '*[^A-Za-z0-9.-]*'
     OR (`port` IS NOT NULL AND (NOT `port` GLOB '[0-9]*' OR `port` GLOB '*[^0-9]*'))
);
