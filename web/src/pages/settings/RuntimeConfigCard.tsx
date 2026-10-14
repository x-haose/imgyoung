import { App as AntdApp, Button, Card, Col, Descriptions, Divider, Flex, Form, Input, InputNumber, Popconfirm, Row, Select, Switch, Tag, Typography } from "antd";
import { useEffect, useMemo, useState } from "react";

import type { FullConfig, RuntimeSettings } from "./fullConfig";
import { updateRuntimeConfig } from "./fullConfig";

/** RuntimeConfigCard 的属性。 */
interface RuntimeConfigCardProps {
  config: FullConfig | undefined; // 全量生效配置;未加载完成时为 undefined
  onSaved: (next: FullConfig) => void; // 保存成功后回传最新配置,供页面各卡片同步
}

/** 表单值形态:宽度档以字符串数组承载(tags 输入),列表字段非空化,其余与 RuntimeSettings 同键同型。 */
type FormValues = Omit<RuntimeSettings, "process_widths" | "process_formats" | "cdn_cloudflare_api_token"> & {
  process_widths: string[];
  process_formats: string[];
  cdn_cloudflare_api_token: string; // 留空表示不修改;仅在用户输入时随 patch 提交
};

/** settings → 表单值:宽度档数字转字符串,凭证输入框恒置空。 */
function toFormValues(s: RuntimeSettings): FormValues {
  return {
    ...s,
    process_widths: (s.process_widths ?? []).map(String),
    process_formats: s.process_formats ?? [],
    cdn_cloudflare_api_token: "",
  };
}

/** 表单值 → 待提交取值:宽度档解析回正整数数组(去重、丢弃非法项)。 */
function fromFormValue(key: string, value: unknown): unknown {
  if (key !== "process_widths") {
    return value;
  }
  const widths = (value as string[])
    .map((v) => Number.parseInt(v, 10))
    .filter((n) => Number.isFinite(n) && n > 0);
  return [...new Set(widths)];
}

/** 分组小节标题:主标题 + 弱化的补充说明。 */
function GroupTitle({ title, hint }: { title: string; hint?: string }) {
  return (
    <Flex align="baseline" gap={10} style={{ marginBottom: 4 }}>
      <Typography.Text strong>{title}</Typography.Text>
      {hint && (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {hint}
        </Typography.Text>
      )}
    </Flex>
  );
}

/** 数值输入的通用列封装:统一栅格跨度、铺满宽度与必填校验(清空不允许提交,防被当作 0 保存)。 */
function NumField(props: {
  name: keyof FormValues;
  label: string;
  unit: string;
  min: number;
  max?: number;
  tooltip?: string;
}) {
  return (
    <Col xs={24} sm={12} xl={6}>
      <Form.Item
        name={props.name}
        label={props.label}
        tooltip={props.tooltip}
        rules={[{ required: true, message: "不可为空" }]}
      >
        <InputNumber min={props.min} max={props.max} addonAfter={props.unit} style={{ width: "100%" }} />
      </Form.Item>
    </Col>
  );
}

/** 运行配置卡片:分组可编辑表单,保存即热生效;引导配置只读展示(仅管理员)。 */
export function RuntimeConfigCard({ config, onSaved }: RuntimeConfigCardProps) {
  const [form] = Form.useForm<FormValues>();
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const { message } = AntdApp.useApp();

  const baseline = useMemo(
    () => (config ? toFormValues(config.settings) : undefined),
    [config],
  );

  useEffect(() => {
    if (baseline) {
      form.setFieldsValue(baseline);
      setDirty(false);
    }
  }, [baseline, form]);

  const save = async () => {
    if (!baseline) {
      return;
    }
    try {
      await form.validateFields();
    } catch {
      message.error("存在未通过校验的字段,请检查表单");
      return;
    }
    const values = form.getFieldsValue(true) as FormValues;
    const patch: Record<string, unknown> = {};
    for (const key of Object.keys(baseline) as (keyof FormValues)[]) {
      if (key === "cdn_cloudflare_api_token") {
        continue; // 凭证字段单独按"非空才提交"处理
      }
      const next = fromFormValue(key, values[key]);
      const base = fromFormValue(key, baseline[key]);
      if (JSON.stringify(next) !== JSON.stringify(base)) {
        patch[key] = next;
      }
    }
    if (values.cdn_cloudflare_api_token.trim() !== "") {
      patch.cdn_cloudflare_api_token = values.cdn_cloudflare_api_token.trim();
    }
    if (Object.keys(patch).length === 0) {
      message.info("没有需要保存的修改");
      setDirty(false);
      return;
    }
    setSaving(true);
    try {
      const next = await updateRuntimeConfig(patch);
      onSaved(next);
      message.success(`已保存 ${Object.keys(patch).length} 项修改,即时生效`);
    } catch (err) {
      message.error(err instanceof Error ? err.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const clearToken = async () => {
    setSaving(true);
    try {
      const next = await updateRuntimeConfig({ cdn_cloudflare_api_token: "" });
      onSaved(next);
      message.success("已清除 Cloudflare API Token");
    } catch (err) {
      message.error(err instanceof Error ? err.message : "清除失败");
    } finally {
      setSaving(false);
    }
  };

  const derived = config?.derived;
  return (
    <Card
      title="运行配置"
      loading={!config}
      extra={
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          保存即热生效,无需重启
        </Typography.Text>
      }
    >
      <Form<FormValues>
        form={form}
        layout="vertical"
        requiredMark={false}
        onValuesChange={() => setDirty(true)}
      >
        <GroupTitle title="服务与访问" />
        <Row gutter={[16, 0]}>
          <Col xs={24} sm={12} xl={6}>
            <Form.Item
              name="access_mode"
              label="访问模式"
              tooltip="proxy 经服务代理回源,可用按需处理;redirect 对允许直链的渠道 302,省服务器流量"
            >
              <Select
                options={[
                  { value: "proxy", label: "proxy(代理)" },
                  { value: "redirect", label: "redirect(302 省流量)" },
                ]}
              />
            </Form.Item>
          </Col>
          <Col xs={24} sm={12} xl={10}>
            <Form.Item
              name="base_url"
              label="对外站点地址"
              tooltip="留空按请求域名推导;经反代 / CDN 后对外域名与请求域名不一致时必须配置"
            >
              <Input placeholder="留空 = 按请求域名推导,如 https://img.example.com" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={12} xl={4}>
            <Form.Item
              name="min_success"
              label="最少同步副本"
              tooltip="上传返回成功所需的最少同步副本数"
              rules={[{ required: true, message: "不可为空" }]}
            >
              <InputNumber min={1} addonAfter="份" style={{ width: "100%" }} />
            </Form.Item>
          </Col>
          <Col xs={24} sm={12} xl={4}>
            <Form.Item
              name="encrypt_new_channels"
              label="新渠道默认加密"
              tooltip="新建渠道未显式选择时的默认值"
            >
              <Switch />
            </Form.Item>
          </Col>
        </Row>

        <Divider style={{ margin: "4px 0 16px" }} />
        <GroupTitle title="上传限制" />
        <Row gutter={[16, 0]}>
          <NumField name="max_upload_mb" label="单图上限" unit="MB" min={1} />
          <NumField name="max_pixels_mp" label="像素预算" unit="百万像素" min={1} tooltip="宽 × 高上限,防超大图打满内存" />
          <NumField name="max_concurrent_uploads" label="上传并发上限" unit="个" min={1} />
          <NumField name="upload_rate_per_min" label="上传频率上限" unit="次/分钟" min={0} tooltip="按用户计,0 表示不启用" />
        </Row>

        <Divider style={{ margin: "4px 0 16px" }} />
        <GroupTitle title="生命周期" />
        <Row gutter={[16, 0]}>
          <NumField name="default_ttl_hours" label="默认有效期" unit="小时" min={0} tooltip="0 表示永不过期;单次上传与用户级默认可覆盖" />
          <NumField name="cold_cleanup_days" label="冷图清理阈值" unit="天" min={0} tooltip="超期未被公开访问的图进入待清理裁决;0 表示不启用" />
          <NumField name="delete_grace_minutes" label="删除宽限" unit="分钟" min={0} tooltip="引用归零到物理删除的等待;0 表示立即" />
          <NumField name="cleanup_sweep_seconds" label="清理扫描间隔" unit="秒" min={1} />
          <NumField name="activity_flush_seconds" label="活跃度落库间隔" unit="秒" min={1} />
          <Col xs={24} sm={12} xl={6}>
            <Form.Item
              name="ttl_renew_on_access"
              label="访问即续期"
              tooltip="公开访问把带有效期图片的到期时刻按其时长顺延"
            >
              <Switch />
            </Form.Item>
          </Col>
        </Row>

        <Divider style={{ margin: "4px 0 16px" }} />
        <GroupTitle title="按需处理" hint="仅 proxy 访问模式生效;档位白名单化防任意参数打满 CPU" />
        <Row gutter={[16, 0]}>
          <Col xs={24} xl={12}>
            <Form.Item
              name="process_widths"
              label="宽度档"
              tooltip="允许的 ?w= 取值(像素),档外请求 400;清空表示不允许缩放"
            >
              <Select mode="tags" tokenSeparators={[",", " "]} placeholder="输入宽度后回车,如 400" suffixIcon={null} open={false} />
            </Form.Item>
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <Form.Item name="process_formats" label="目标格式" tooltip="允许的 ?format= 取值;gif 仅收发原图不处理">
              <Select
                mode="multiple"
                options={["webp", "jpeg", "png"].map((f) => ({ value: f, label: f }))}
              />
            </Form.Item>
          </Col>
          <Col xs={24} sm={12} xl={6}>
            <Form.Item name="exif_policy" label="EXIF 默认档" tooltip="单次上传的表单字段可覆盖">
              <Select
                options={[
                  { value: "passthrough", label: "passthrough(保留)" },
                  { value: "strip", label: "strip(剥离元数据)" },
                ]}
              />
            </Form.Item>
          </Col>
          <NumField name="process_quality_webp" label="WebP 质量档" unit="/100" min={0} max={100} />
          <NumField name="process_quality_jpeg" label="JPEG 质量档" unit="/100" min={1} max={100} />
          <NumField name="process_webp_method" label="WebP 速度权衡" unit="/6" min={0} max={6} tooltip="越大越慢、体积越小" />
          <NumField name="process_workers" label="处理并发上限" unit="个" min={1} />
          <NumField name="process_timeout_ms" label="处理超时" unit="毫秒" min={1} tooltip="超时回退原图" />
          <NumField name="variants_cache_mb" label="衍生图缓存上限" unit="MB" min={1} tooltip="超过按最近访问 LRU 淘汰" />
        </Row>

        <Divider style={{ margin: "4px 0 16px" }} />
        <GroupTitle title="安全" />
        <Row gutter={[16, 0]}>
          <Col xs={24} sm={12} xl={6}>
            <Form.Item
              name="allow_empty_referer"
              label="空 Referer 放行"
              tooltip="防盗链白名单启用时,无 Referer 的直接访问是否放行"
            >
              <Switch />
            </Form.Item>
          </Col>
          <Col xs={24} xl={18}>
            <Form.Item label="登录限速">
              <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                每个 IP / 用户名突发 {derived?.login_burst ?? "-"} 次,之后每{" "}
                {derived?.login_refill_seconds ?? "-"} 秒恢复 1 次(内置策略)
              </Typography.Text>
            </Form.Item>
          </Col>
        </Row>

        <Divider style={{ margin: "4px 0 16px" }} />
        <GroupTitle title="可靠性" />
        <Row gutter={[16, 0]}>
          <NumField name="backup_keep" label="备份保留份数" unit="份" min={1} />
          <NumField name="backup_interval_hours" label="备份间隔" unit="小时" min={1} />
          <NumField name="health_check_seconds" label="健康检查间隔" unit="秒" min={1} />
          <NumField name="usage_report_seconds" label="用量上报间隔" unit="秒" min={1} />
          <NumField name="channel_quota_gb" label="渠道用量告警阈值" unit="GB" min={0} tooltip="超过则日志告警;0 表示只上报不告警" />
          <NumField name="dedup_scan_seconds" label="查重扫描间隔" unit="秒" min={1} />
          <NumField name="dedup_phash_max_distance" label="相似判定阈值" unit="汉明距离" min={0} tooltip="越大越宽松;0 表示不做相似扫描" />
          <Col xs={24} sm={12} xl={6}>
            <Form.Item label="副本对账间隔">
              <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                {derived?.reconcile_interval_seconds ?? "-"} 秒(内置策略)
              </Typography.Text>
            </Form.Item>
          </Col>
        </Row>

        <Divider style={{ margin: "4px 0 16px" }} />
        <GroupTitle
          title="CDN 缓存清理"
          hint="删除级联的 Cloudflare purge;需同时配置 Zone ID、API Token 与对外站点地址"
        />
        <Row gutter={[16, 0]}>
          <Col xs={24} sm={12} xl={8}>
            <Form.Item name="cdn_cloudflare_zone_id" label="Cloudflare Zone ID">
              <Input placeholder="留空 = 不启用" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={12} xl={8}>
            <Form.Item
              name="cdn_cloudflare_api_token"
              label="Cloudflare API Token"
              tooltip="需 Zone.Cache Purge 权限;加密存储,任何界面不再回显"
            >
              <Input.Password
                placeholder={derived?.cdn_api_token_set ? "已配置(留空保持不变)" : "未配置"}
                autoComplete="new-password"
              />
            </Form.Item>
          </Col>
          <Col xs={24} xl={8}>
            <Form.Item label="当前状态">
              <Flex align="center" gap={8}>
                <Tag color={derived?.cdn_purge_enabled ? "success" : "default"}>
                  {derived?.cdn_purge_enabled ? "已启用" : "未启用"}
                </Tag>
                {derived?.cdn_api_token_set && (
                  <Popconfirm
                    title="清除已配置的 API Token?"
                    description="清除后 CDN purge 立即停用。"
                    okButtonProps={{ danger: true }}
                    onConfirm={() => void clearToken()}
                  >
                    <Button size="small" danger type="text">
                      清除 Token
                    </Button>
                  </Popconfirm>
                )}
              </Flex>
            </Form.Item>
          </Col>
        </Row>
      </Form>

      <Flex justify="space-between" align="center" wrap gap={12}>
        <Descriptions
          size="small"
          column={2}
          items={[
            { key: "listen", label: "监听地址", children: config?.bootstrap.listen ?? "-" },
            { key: "data_dir", label: "数据目录", children: config?.bootstrap.data_dir ?? "-" },
            { key: "trusted_proxies", label: "信任的反代网段", children: config?.bootstrap.trusted_proxies ?? "-" },
            { key: "metrics_allow", label: "/metrics 放行网段", children: config?.bootstrap.metrics_allow ?? "-" },
          ]}
          styles={{ root: { maxWidth: 640 }, label: { whiteSpace: "nowrap" } }}
          title={
            <Typography.Text type="secondary" style={{ fontSize: 12, fontWeight: 400 }}>
              引导配置(环境变量 IMGYOUNG_*,改动需重启)
            </Typography.Text>
          }
        />
        <Flex gap={8}>
          <Button
            disabled={!dirty || saving}
            onClick={() => {
              if (baseline) {
                form.setFieldsValue(baseline);
                setDirty(false);
              }
            }}
          >
            还原
          </Button>
          <Button type="primary" loading={saving} disabled={!dirty} onClick={() => void save()}>
            保存修改
          </Button>
        </Flex>
      </Flex>
    </Card>
  );
}
