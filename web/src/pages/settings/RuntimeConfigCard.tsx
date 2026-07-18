import { Alert, Card, Descriptions, Tag, Typography } from "antd";
import type { ReactNode } from "react";

import { useFullConfig, type FullConfig } from "./fullConfig";

/** onOff 布尔值的友好展示:开启/关闭。 */
function onOff(value: boolean): ReactNode {
  return value ? <Tag color="success">开启</Tag> : <Tag>关闭</Tag>;
}

/** orMeaning 数值为 0 时展示其特殊语义,否则展示"数值 + 单位"。 */
function orMeaning(value: number, unit: string, zeroMeaning: string): string {
  return value === 0 ? zeroMeaning : `${value} ${unit}`;
}

/** group 单个配置组的渲染描述:标题 + 条目。 */
interface ConfigGroup {
  title: string;
  items: { label: string; value: ReactNode }[];
}

/** buildGroups 把接口返回的分组配置翻译为可读条目(单位与零值语义在此归一)。 */
function buildGroups(cfg: FullConfig): ConfigGroup[] {
  return [
    {
      title: "服务",
      items: [
        { label: "监听地址", value: cfg.server.listen },
        { label: "对外站点地址", value: cfg.server.base_url || "未设置(返回相对路径)" },
        {
          label: "访问模式",
          value: cfg.server.access_mode === "redirect" ? "redirect(302 省流量)" : "proxy(代理)",
        },
      ],
    },
    {
      title: "上传限制",
      items: [
        { label: "单图上限", value: `${cfg.upload.max_upload_mb} MB` },
        { label: "像素预算", value: `${cfg.upload.max_pixels_mp} 百万像素` },
        { label: "上传并发上限", value: `${cfg.upload.max_concurrent_uploads}` },
        {
          label: "上传频率上限",
          value: orMeaning(cfg.upload.upload_rate_per_min, "次/分钟(每用户)", "不限"),
        },
      ],
    },
    {
      title: "生命周期",
      items: [
        { label: "默认有效期", value: orMeaning(cfg.lifecycle.default_ttl_hours, "小时", "永不过期") },
        { label: "访问即续期", value: onOff(cfg.lifecycle.ttl_renew_on_access) },
        { label: "冷图清理阈值", value: orMeaning(cfg.lifecycle.cold_cleanup_days, "天未访问", "未启用") },
        { label: "删除宽限", value: orMeaning(cfg.lifecycle.delete_grace_minutes, "分钟", "立即删除") },
        { label: "清理扫描间隔", value: `${cfg.lifecycle.cleanup_sweep_seconds} 秒` },
      ],
    },
    {
      title: "按需处理",
      items: [
        {
          label: "宽度档",
          value: cfg.processing.process_widths?.length
            ? cfg.processing.process_widths.join(" / ") + " px"
            : "不允许缩放",
        },
        { label: "目标格式", value: cfg.processing.process_formats?.join(" / ") ?? "—" },
        { label: "WebP 质量档", value: `${cfg.processing.process_quality_webp}` },
        { label: "JPEG 质量档", value: `${cfg.processing.process_quality_jpeg}` },
        {
          label: "EXIF 档位",
          value: cfg.processing.exif_policy === "strip" ? "strip(净化)" : "passthrough(保留)",
        },
      ],
    },
    {
      title: "安全",
      items: [
        { label: "空 Referer 放行", value: onOff(cfg.security.allow_empty_referer) },
        {
          label: "登录限速",
          value: `突发 ${cfg.security.login_burst} 次,之后每 ${cfg.security.login_refill_seconds} 秒恢复 1 次`,
        },
      ],
    },
    {
      title: "可靠性",
      items: [
        { label: "备份保留份数", value: `${cfg.reliability.backup_keep} 份` },
        { label: "备份间隔", value: `${cfg.reliability.backup_interval_hours} 小时` },
        { label: "渠道健康检查间隔", value: `${cfg.reliability.health_check_seconds} 秒` },
        { label: "副本对账间隔", value: `${cfg.reliability.reconcile_interval_seconds} 秒` },
      ],
    },
  ];
}

/** 运行配置卡片:分组只读展示当前生效配置(仅管理员;写路径是配置文件/环境变量)。 */
export function RuntimeConfigCard() {
  const config = useFullConfig();

  return (
    <Card title="运行配置" loading={!config}>
      <Alert
        type="info"
        showIcon
        message="配置来自配置文件 / 环境变量(IMGYOUNG_*),修改后需重启进程生效。"
        style={{ marginBottom: 20 }}
      />
      {config &&
        buildGroups(config).map((group) => (
          <div key={group.title} style={{ marginBottom: 20 }}>
            <Typography.Text strong style={{ display: "block", marginBottom: 8 }}>
              {group.title}
            </Typography.Text>
            <Descriptions
              size="small"
              bordered
              column={{ xs: 1, sm: 2 }}
              items={group.items.map((item) => ({
                key: item.label,
                label: item.label,
                children: item.value,
              }))}
              styles={{ label: { whiteSpace: "nowrap" } }}
            />
          </div>
        ))}
    </Card>
  );
}
