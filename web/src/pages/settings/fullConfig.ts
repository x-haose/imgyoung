import { useCustom } from "@refinedev/core";

/** 全量非敏感生效配置(GET /api/admin/config/full,仅管理员),按组呈现。 */
export interface FullConfig {
  server: {
    listen: string; // HTTP 监听地址
    base_url: string; // 对外站点地址;空表示返回相对路径
    access_mode: string; // 访问模式:proxy / redirect
  };
  upload: {
    max_upload_mb: number; // 单图上限(MB)
    max_pixels_mp: number; // 像素预算(百万像素)
    max_concurrent_uploads: number; // 上传并发上限
    upload_rate_per_min: number; // 每用户每分钟上传次数上限;0 表示不启用
  };
  lifecycle: {
    default_ttl_hours: number; // 全局默认 TTL(小时);0 表示永不过期
    ttl_renew_on_access: boolean; // 公开访问是否顺延带 TTL 图片的到期时刻
    cold_cleanup_days: number; // 冷图待清理阈值(天);0 表示未启用
    delete_grace_minutes: number; // 引用归零到物理删的宽限(分钟);0 表示立即
    cleanup_sweep_seconds: number; // 生命周期清理扫描间隔(秒)
  };
  processing: {
    process_widths: number[] | null; // 允许的宽度档(像素);空表示不允许缩放
    process_formats: string[] | null; // 允许的目标格式
    process_quality_webp: number; // webp 质量档
    process_quality_jpeg: number; // jpeg 质量档
    exif_policy: string; // 全局 EXIF 档位
  };
  security: {
    allow_empty_referer: boolean; // 空 Referer 是否放行
    login_burst: number; // 每个 IP / 用户名允许的突发登录尝试次数
    login_refill_seconds: number; // 突发额度用尽后每恢复一次尝试的间隔(秒)
  };
  reliability: {
    backup_keep: number; // 元数据库备份保留份数
    backup_interval_hours: number; // 备份间隔(小时)
    health_check_seconds: number; // 渠道健康检查间隔(秒)
    reconcile_interval_seconds: number; // 副本对账间隔(秒)
  };
}

/**
 * 拉取全量生效配置(仅管理员可调),会话期内共享缓存;
 * 配置只随进程重启变化,无需重取。加载中或失败返回 undefined。
 */
export function useFullConfig(): FullConfig | undefined {
  const { query } = useCustom<FullConfig>({
    url: "/api/admin/config/full",
    method: "get",
    queryOptions: { staleTime: Infinity },
    errorNotification: false,
  });
  return query.data?.data;
}
