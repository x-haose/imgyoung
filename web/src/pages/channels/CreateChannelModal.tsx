import { MinusCircleOutlined, PlusOutlined } from "@ant-design/icons";
import { useList } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Card,
  Divider,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Switch,
  Typography,
  theme,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";

/** select 字段的一个选项。 */
interface FieldOption {
  label: string;
  value: string;
}

/** 模板的一个用户输入字段。 */
interface TemplateField {
  key: string;
  label: string;
  type: "text" | "password" | "select";
  required: boolean;
  placeholder?: string;
  help?: string;
  options?: FieldOption[];
}

/** 渠道模板(后端下发,不含 provider 等预设)。 */
interface ChannelTemplate {
  id: string;
  name: string;
  description: string;
  driver: string;
  fields: TemplateField[];
}

/** CreateChannelModal 的属性。 */
interface CreateChannelModalProps {
  open: boolean; // 是否可见
  onClose: () => void; // 关闭
  onCreated: () => void; // 创建成功回调(触发列表刷新)
}

/** 自定义模式的哨兵 id。 */
const CUSTOM = "custom";

/** 渠道创建弹窗:先选模板(内置 S3 系厂商或自定义),再按模板渲染专属表单。 */
export function CreateChannelModal({ open, onClose, onCreated }: CreateChannelModalProps) {
  const [tplId, setTplId] = useState("");
  const [saving, setSaving] = useState(false);
  const { message } = AntdApp.useApp();
  const { token } = theme.useToken();
  const [form] = Form.useForm();
  // 加密与 302 直链互斥(密文不能直出),一方开启即禁用另一方并就地说明,不等后端打回。
  const encrypted = Form.useWatch("encrypted", form) as boolean | undefined;
  const allowDirectLink = Form.useWatch("allow_direct_link", form) as boolean | undefined;

  const templates = useList<ChannelTemplate>({
    resource: "channel-templates",
    pagination: { mode: "off" },
  });
  const tpls = templates.result?.data ?? [];
  const selected = tpls.find((t) => t.id === tplId);

  const reset = () => {
    form.resetFields();
    setTplId("");
  };

  const submit = async (values: Record<string, unknown>) => {
    setSaving(true);
    try {
      const common = {
        name: values.name,
        priority: values.priority ?? 100,
        encrypted: values.encrypted ?? false,
        allow_direct_link: values.allow_direct_link ?? false,
        auto_create_bucket: values.auto_create_bucket ?? false,
      };
      let body: Record<string, unknown>;
      if (tplId === CUSTOM) {
        const config: Record<string, string> = {};
        for (const kv of (values.config as { key: string; value: string }[]) ?? []) {
          if (kv?.key) {
            config[kv.key] = kv.value ?? "";
          }
        }
        body = { ...common, template: CUSTOM, driver: values.driver, config };
      } else {
        body = { ...common, template: tplId, inputs: values.inputs ?? {} };
      }
      await apiFetch("/api/admin/channels", { method: "POST", body: JSON.stringify(body) });
      message.success("渠道已创建");
      reset();
      onCreated();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "创建失败");
    } finally {
      setSaving(false);
    }
  };

  const close = () => {
    reset();
    onClose();
  };

  return (
    <Modal
      open={open}
      title="新建渠道"
      onCancel={close}
      onOk={() => form.submit()}
      confirmLoading={saving}
      okText="创建"
      okButtonProps={{ disabled: tplId === "" }}
      width={640}
    >
      <Typography.Text type="secondary">选择存储类型</Typography.Text>
      <Flex wrap gap={8} style={{ margin: "10px 0 4px" }}>
        {[...tpls, { id: CUSTOM, name: "自定义", description: "手动填 rclone 参数" } as ChannelTemplate].map(
          (t) => {
            const active = tplId === t.id;
            return (
              <Card
                key={t.id}
                size="small"
                hoverable
                onClick={() => {
                  form.resetFields();
                  setTplId(t.id);
                }}
                styles={{ body: { padding: "8px 12px" } }}
                style={{
                  width: 140,
                  cursor: "pointer",
                  borderColor: active ? token.colorPrimary : token.colorBorderSecondary,
                  background: active ? token.colorPrimaryBg : undefined,
                }}
              >
                <Typography.Text strong style={{ display: "block", fontSize: 13 }}>
                  {t.name}
                </Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  {t.description}
                </Typography.Text>
              </Card>
            );
          },
        )}
      </Flex>

      {tplId !== "" && (
        <>
          <Divider style={{ margin: "12px 0" }} />
          <Form
            form={form}
            layout="vertical"
            onFinish={submit}
            initialValues={{ priority: 100, encrypted: false, allow_direct_link: false, auto_create_bucket: false }}
          >
            <Form.Item name="name" label="渠道名" rules={[{ required: true, message: "请输入渠道名" }]}>
              <Input placeholder="如 r2-primary" />
            </Form.Item>

            {selected?.fields.map((f) => (
              <Form.Item
                key={f.key}
                name={["inputs", f.key]}
                label={f.label}
                help={f.help}
                rules={f.required ? [{ required: true, message: `请填写${f.label}` }] : undefined}
              >
                {f.type === "password" ? (
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

            {tplId === CUSTOM && <CustomFields />}

            <Flex gap={24} wrap>
              <Form.Item name="priority" label="优先级" style={{ width: 120 }}>
                <InputNumber min={0} max={1000} style={{ width: "100%" }} />
              </Form.Item>
              <Form.Item
                name="encrypted"
                label="服务端加密"
                valuePropName="checked"
                extra={allowDirectLink ? "已开直链的渠道无法加密:密文不能直出" : undefined}
              >
                <Switch disabled={allowDirectLink === true} />
              </Form.Item>
              <Form.Item
                name="allow_direct_link"
                label="允许 302 直链"
                valuePropName="checked"
                extra={encrypted ? "加密渠道无法 302 直链:密文不能直出" : undefined}
              >
                <Switch disabled={encrypted === true} />
              </Form.Item>
            </Flex>
            <Form.Item
              name="auto_create_bucket"
              label="自动创建存储桶"
              valuePropName="checked"
              extra="创建时尝试建桶,需密钥有建桶权限;桶已存在或用只读/单桶令牌请关闭"
            >
              <Switch />
            </Form.Item>
          </Form>
        </>
      )}
    </Modal>
  );
}

/** CustomFields 自定义模式:rclone 后端名 + 键值对配置。 */
function CustomFields() {
  return (
    <>
      <Form.Item name="driver" label="rclone 驱动" rules={[{ required: true, message: "请输入驱动名" }]}>
        <Input placeholder="s3 / local / webdav …" />
      </Form.Item>
      <Form.Item label="后端参数" extra="rclone 后端参数;local 用 path,S3 类用 access_key_id 等">
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
