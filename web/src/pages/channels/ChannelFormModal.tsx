import { ArrowLeftOutlined, MinusCircleOutlined, PlusOutlined } from "@ant-design/icons";
import { useList } from "@refinedev/core";
import {
  Alert,
  App as AntdApp,
  Button,
  Divider,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Skeleton,
  Space,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import { useEffect, useState } from "react";

import { apiFetch } from "../../api/http";
import { OAuthField } from "./OAuthField";
import { CUSTOM, TemplatePicker, TypeBadge } from "./TemplatePicker";
import type { ChannelItem, ChannelTemplate, TemplateField } from "./types";

/** 单渠道配置的编辑视图(敏感值已哨兵掩码)。 */
interface EditConfig {
  template: string; // 配置所用模板 id;空 = 自定义
  driver: string; // rclone 后端名
  config: Record<string, string>; // 掩码后的配置
  inputs?: Record<string, string>; // 模板表单字段值;仅模板渠道
}

/** 测试连接的响应体。 */
interface TestResult {
  ok: boolean; // 探测是否通过
  error?: string; // 失败原因(人话化)
}

/** 创建模式服务端加密的三态选择;继承态不提交该字段,由后端用全局默认固化。 */
type EncryptChoice = "inherit" | "on" | "off";

/** 自定义模式的键值对表单行。 */
interface ConfigEntry {
  key?: string;
  value?: string;
}

/** 表单值形态(创建与编辑共用;encrypted / auto_create_bucket 仅创建模式渲染)。 */
interface FormValues {
  name: string;
  priority?: number;
  encrypted?: EncryptChoice;
  allow_direct_link?: boolean;
  auto_create_bucket?: boolean;
  inputs?: Record<string, string>;
  driver?: string;
  config?: ConfigEntry[];
}

/** ChannelFormModal 的属性。 */
interface ChannelFormModalProps {
  open: boolean; // 是否可见
  channel: ChannelItem | null; // 编辑目标;null = 创建模式
  onClose: () => void; // 关闭
  onSaved: () => void; // 保存成功回调(触发列表刷新)
}

/** 敏感配置值的回读哨兵:原样回传表示保留原凭证,与后端 channel.SecretPlaceholder 一致。 */
const SECRET_PLACEHOLDER = "__imgyoung_secret_keep__";

/** 创建模式的表单初值;编辑模式的预填在 Form 挂载后以 setFieldsValue 覆盖。 */
const CREATE_DEFAULTS: FormValues = {
  name: "",
  priority: 100,
  encrypted: "inherit",
  allow_direct_link: false,
  auto_create_bucket: false,
};

/**
 * ChannelFormModal 渠道创建/编辑弹窗:先选模板(内置 S3 系厂商或自定义),再按模板渲染
 * 专属表单。编辑模式回读哨兵掩码的配置预填,支持不重录凭证保存;两种模式都可
 * 「测试连接」做不落库深探测,编辑模式带渠道 ID 由后端把哨兵还原为原凭证后再测。
 */
export function ChannelFormModal({ open, channel, onClose, onSaved }: ChannelFormModalProps) {
  const isEdit = channel !== null;
  const [tplId, setTplId] = useState("");
  const [prefill, setPrefill] = useState<FormValues | null>(null);
  const [loadingConfig, setLoadingConfig] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<TestResult | null>(null);
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm<FormValues>();
  const encryptChoice = Form.useWatch("encrypted", form);
  const allowDirectLink = Form.useWatch("allow_direct_link", form);
  const templateBaseURL = Form.useWatch(["inputs", "public_base_url"], form) as string | undefined;
  const customConfig = Form.useWatch("config", form) as ConfigEntry[] | undefined;
  // 授权端点随已填字段变化(如 onedrive 的 region),须订阅而非渲染时快照,
  // 否则先填区域再点授权会拿旧值,授权打到错误的区域端点。
  const templateInputs = Form.useWatch("inputs", form) as Record<string, string> | undefined;

  const templates = useList<ChannelTemplate>({
    resource: "channel-templates",
    pagination: { mode: "off" },
  });
  const tpls = templates.result?.data ?? [];
  const selected = tpls.find((t) => t.id === tplId);
  // 网盘没有桶的概念,也给不出稳定公开直链(恒走代理),两个开关对其无意义。
  const isDrive = selected?.category === "drive";

  // 加密与 302 直链互斥(密文不能直出):创建模式看三态选择,编辑模式看渠道固有属性。
  const encryptedOn = isEdit ? channel.encrypted : encryptChoice === "on";
  // 直链联动:开了直链但配置里没有 public_base_url 时,302 不会生效、访问回落代理。
  const publicBaseURL =
    tplId === CUSTOM
      ? ((customConfig ?? []).find((kv) => kv?.key === "public_base_url")?.value ?? "")
      : (templateBaseURL ?? "");
  const directLinkNeedsBaseURL = allowDirectLink === true && publicBaseURL.trim() === "";

  // 编辑模式打开时回读配置预填;channel 是父组件打开弹窗时的快照,弹窗期间引用稳定。
  useEffect(() => {
    if (!open || !channel) {
      return;
    }
    let cancelled = false;
    setLoadingConfig(true);
    apiFetch<EditConfig>(`/api/admin/channels/${channel.id}/config`)
      .then((cfg) => {
        if (cancelled) {
          return;
        }
        setPrefill({
          name: channel.name,
          priority: channel.priority,
          allow_direct_link: channel.allow_direct_link,
          inputs: cfg.inputs ?? {},
          driver: cfg.driver,
          config: Object.entries(cfg.config ?? {}).map(([key, value]) => ({ key, value })),
        });
        setTplId(cfg.template === "" ? CUSTOM : cfg.template);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          message.error(err instanceof Error ? err.message : "读取渠道配置失败");
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingConfig(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open, channel, message]);

  // 预填须在 Form 挂载后应用:Form 仅在选定模板后渲染,提前 set 会打在未连接的实例上。
  // 应用后立即清空 prefill,保证只生效一次:tplId 是依赖项,切换模板会重跑本 effect,
  // 若 prefill 仍在会把旧模板的 inputs(含密码哨兵)和旧 name/priority 重灌回表单。
  useEffect(() => {
    if (prefill && tplId !== "") {
      form.setFieldsValue(prefill);
      setPrefill(null);
    }
  }, [prefill, tplId, form]);

  const reset = () => {
    form.resetFields();
    setTplId("");
    setPrefill(null);
    setLoadingConfig(false); // 配置加载中关闭弹窗时,落定回调被 cancelled 拦截,这里兜底复位
    setTestResult(null);
  };

  // 返回选类型网格。重选类型时 switchTpl 会按新模板重置表单,故此处无需清理。
  const goBack = () => {
    setTplId("");
    setTestResult(null);
  };

  // 切换模板保留通用字段、清空模板专属字段;编辑模式切换后凭证需重新填写。
  const switchTpl = (id: string) => {
    // 首次选择时 Form 尚未挂载:跳过保留,否则会把 undefined 写进 store 而挡掉 initialValues。
    const common =
      tplId === ""
        ? {}
        : form.getFieldsValue(["name", "priority", "encrypted", "allow_direct_link", "auto_create_bucket"]);
    form.resetFields();
    form.setFieldsValue(common);
    setTplId(id);
    setTestResult(null);
  };

  // 配置部分的请求体(创建/编辑/测试同构):模板模式 template+inputs,自定义 driver+config。
  const configBody = (values: FormValues): Record<string, unknown> => {
    if (tplId !== CUSTOM) {
      return { template: tplId, inputs: values.inputs ?? {} };
    }
    const config: Record<string, string> = {};
    for (const kv of values.config ?? []) {
      if (kv?.key) {
        config[kv.key] = kv.value ?? "";
      }
    }
    return { template: CUSTOM, driver: values.driver, config };
  };

  // 测试连接:不落库深探测;编辑模式带渠道 ID,后端把哨兵值还原为原凭证后再测。
  const test = async () => {
    let values: FormValues;
    try {
      values = await form.validateFields();
    } catch {
      return; // 校验失败,表单已就地标红
    }
    setTesting(true);
    setTestResult(null);
    try {
      const body = { ...configBody(values), ...(channel ? { channel_id: channel.id } : {}) };
      const resp = await apiFetch<TestResult>("/api/admin/channels/test", {
        method: "POST",
        body: JSON.stringify(body),
      });
      setTestResult(resp);
    } catch (err) {
      setTestResult({ ok: false, error: err instanceof Error ? err.message : "测试请求失败" });
    } finally {
      setTesting(false);
    }
  };

  const submit = async (values: FormValues) => {
    setSaving(true);
    try {
      const common = {
        name: values.name,
        priority: values.priority ?? 100,
        allow_direct_link: values.allow_direct_link ?? false,
        ...configBody(values),
      };
      let lastError: string | undefined;
      if (channel) {
        // encrypted 不回传:后端视为不变更,加密属性创建后不可变。
        const info = await apiFetch<ChannelItem>(`/api/admin/channels/${channel.id}`, {
          method: "PUT",
          body: JSON.stringify(common),
        });
        lastError = info.last_error;
      } else {
        const body: Record<string, unknown> = {
          ...common,
          auto_create_bucket: values.auto_create_bucket ?? false,
        };
        if (values.encrypted === "on" || values.encrypted === "off") {
          body.encrypted = values.encrypted === "on";
        }
        const created = await apiFetch<{ id: number }>("/api/admin/channels", {
          method: "POST",
          body: JSON.stringify(body),
        });
        const list = await apiFetch<{ items: ChannelItem[] }>("/api/admin/channels");
        lastError = list.items.find((c) => c.id === created.id)?.last_error;
      }
      const done = isEdit ? "保存" : "创建";
      if (lastError) {
        message.warning(`渠道已${done},但探测失败:${lastError}`, 6);
      } else {
        message.success(`渠道已${done}`);
      }
      reset();
      onSaved();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "提交失败");
    } finally {
      setSaving(false);
    }
  };

  const close = () => {
    reset();
    onClose();
  };

  // 模板密码字段在编辑模式追加哨兵说明;extra 不遮蔽校验错误(help 会)。
  const fieldExtra = (f: TemplateField): string | undefined => {
    if (isEdit && f.type === "password") {
      return f.help ? `${f.help};保持不变则不必修改,输入新值即替换` : "保持不变则不必修改,输入新值即替换";
    }
    return f.help;
  };

  const directLinkExtra = encryptedOn ? (
    "加密渠道无法 302 直链:密文不能直出"
  ) : directLinkNeedsBaseURL ? (
    <Typography.Text type="warning" style={{ fontSize: 12 }}>
      未配置 public_base_url,302 直链不会生效,访问将回落代理
    </Typography.Text>
  ) : undefined;

  return (
    <Modal
      open={open}
      title={isEdit ? `编辑渠道 ${channel.name}` : "新建渠道"}
      onCancel={close}
      width={640}
      footer={
        tplId === "" ? (
          // 选类型步:未定类型,测试/提交无意义,只留取消
          <Button onClick={close}>取消</Button>
        ) : (
          <Flex justify="space-between" align="center">
            <Button onClick={test} loading={testing}>
              测试连接
            </Button>
            <Space>
              <Button onClick={close}>取消</Button>
              <Button type="primary" loading={saving} onClick={() => form.submit()}>
                {isEdit ? "保存" : "创建"}
              </Button>
            </Space>
          </Flex>
        )
      }
    >
      {loadingConfig ? (
        <Skeleton active paragraph={{ rows: 5 }} />
      ) : tplId === "" ? (
        <TemplatePicker templates={tpls} onSelect={switchTpl} />
      ) : (
        // 配置步:返回头(徽标 + 类型名)+ 专属表单
        <>
          <Flex align="center" gap={12} style={{ marginBottom: 4 }}>
            <Button type="text" size="small" icon={<ArrowLeftOutlined />} onClick={goBack}>
              返回
            </Button>
            <Divider type="vertical" style={{ margin: 0, height: 24 }} />
            <TypeBadge id={tplId} size={32} />
            <div style={{ minWidth: 0 }}>
              <Typography.Text strong style={{ display: "block", fontSize: 14 }}>
                {selected?.name ?? "自定义"}
              </Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {selected?.description ?? "手动填 rclone 参数"}
              </Typography.Text>
            </div>
          </Flex>
          <Divider style={{ margin: "12px 0" }} />
          <>
            <Form
                form={form}
                layout="vertical"
                onFinish={submit}
                onValuesChange={() => setTestResult(null)}
                initialValues={CREATE_DEFAULTS}
              >
                <Form.Item name="name" label="渠道名" rules={[{ required: true, message: "请输入渠道名" }]}>
                  <Input placeholder="如 r2-primary" />
                </Form.Item>

                {selected?.fields.map((f) => (
                  <Form.Item
                    key={f.key}
                    name={["inputs", f.key]}
                    label={f.label}
                    extra={fieldExtra(f)}
                    rules={f.required ? [{ required: true, message: `请填写${f.label}` }] : undefined}
                  >
                    {f.type === "oauth" ? (
                      <OAuthField
                        templateId={selected.id}
                        templateName={selected.name}
                        driver={selected.driver}
                        inputs={templateInputs ?? {}}
                      />
                    ) : f.type === "password" ? (
                      <Input.Password placeholder={f.placeholder} autoComplete="off" />
                    ) : f.type === "select" ? (
                      <Select
                        placeholder="请选择"
                        showSearch
                        optionFilterProp="label"
                        options={f.options?.map((o) => ({ label: o.label, value: o.value }))}
                      />
                    ) : (
                      <Input placeholder={f.placeholder} />
                    )}
                  </Form.Item>
                ))}

                {tplId === CUSTOM && <CustomFields isEdit={isEdit} />}

                <Flex gap={24} wrap>
                  <Form.Item name="priority" label="优先级" style={{ width: 120 }}>
                    <InputNumber min={0} max={1000} style={{ width: "100%" }} />
                  </Form.Item>
                  {isEdit ? (
                    <Form.Item label="服务端加密">
                      <Tooltip title="加密属性创建后不可变(变更会造成密文/明文混存);如需调整请新建渠道并迁移数据">
                        <Tag
                          color={channel.encrypted ? "geekblue" : "default"}
                          style={{ cursor: "help", marginTop: 5 }}
                        >
                          {channel.encrypted ? "已加密" : "未加密"}
                        </Tag>
                      </Tooltip>
                    </Form.Item>
                  ) : (
                    <Form.Item
                      name="encrypted"
                      label="服务端加密"
                      style={{ width: 168 }}
                      extra={allowDirectLink === true ? "已开直链的渠道无法强制加密:密文不能直出" : undefined}
                    >
                      <Select
                        options={[
                          { value: "inherit", label: "继承全局默认" },
                          { value: "on", label: "强制开启", disabled: allowDirectLink === true },
                          { value: "off", label: "强制关闭" },
                        ]}
                      />
                    </Form.Item>
                  )}
                  {!isDrive && (
                    <Form.Item
                      name="allow_direct_link"
                      label="允许 302 直链"
                      valuePropName="checked"
                      extra={directLinkExtra}
                    >
                      <Switch disabled={encryptedOn} />
                    </Form.Item>
                  )}
                </Flex>
                {!isEdit && !isDrive && (
                  <Form.Item
                    name="auto_create_bucket"
                    label="自动创建存储桶"
                    valuePropName="checked"
                    extra="创建时尝试建桶,需密钥有建桶权限;桶已存在或用只读/单桶令牌请关闭"
                  >
                    <Switch />
                  </Form.Item>
                )}
              </Form>

              {testResult && (
                <Alert
                  style={{ marginTop: 4 }}
                  type={testResult.ok ? "success" : "error"}
                  showIcon
                  message={
                    testResult.ok
                      ? "连接正常(写读删探测通过)"
                      : `连接失败:${testResult.error ?? "未知原因"}`
                  }
                />
              )}
            </>
          </>
        )}
    </Modal>
  );
}

/** CustomFields 自定义模式:rclone 后端名 + 键值对配置;编辑模式说明哨兵语义。 */
function CustomFields({ isEdit }: { isEdit: boolean }) {
  const base =
    "rclone 后端参数;local 用 path,S3 类用 access_key_id 等,公开访问地址用 public_base_url;密码类参数填明文或 rclone obscure 的输出均可";
  return (
    <>
      <Form.Item name="driver" label="rclone 驱动" rules={[{ required: true, message: "请输入驱动名" }]}>
        <Input placeholder="s3 / local / webdav …" />
      </Form.Item>
      <Form.Item
        label="后端参数"
        extra={isEdit ? `${base};值为 ${SECRET_PLACEHOLDER} 的参数保持原凭证不变,输入新值即替换` : base}
      >
        <Form.List name="config">
          {(fields, { add, remove }) => (
            <Flex vertical gap={8}>
              {fields.map((field) => (
                <Flex key={field.key} gap={8} align="baseline">
                  <Form.Item name={[field.name, "key"]} noStyle>
                    <Input placeholder="键(如 path)" style={{ width: 180 }} />
                  </Form.Item>
                  <Form.Item name={[field.name, "value"]} noStyle>
                    <Input placeholder="值" style={{ flex: 1 }} />
                  </Form.Item>
                  <MinusCircleOutlined onClick={() => remove(field.name)} />
                </Flex>
              ))}
              <Button type="dashed" onClick={() => add()} icon={<PlusOutlined />} block>
                添加参数
              </Button>
            </Flex>
          )}
        </Form.List>
      </Form.Item>
    </>
  );
}
