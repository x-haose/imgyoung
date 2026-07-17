import { MinusCircleOutlined, PlusOutlined } from "@ant-design/icons";
import { useInvalidate, useList } from "@refinedev/core";
import {
  App as AntdApp,
  Button,
  Card,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";

/** 渠道列表项(channel.Info,凭证与配置不出库)。 */
interface ChannelItem {
  id: number;
  name: string;
  driver: string;
  priority: number;
  healthy: boolean;
  encrypted: boolean;
  allow_direct_link: boolean;
}

/** 创建表单值。 */
interface ChannelForm {
  name: string;
  driver: string;
  priority: number;
  encrypted: boolean;
  allow_direct_link: boolean;
  config?: { key: string; value: string }[];
}

/** 渠道页:列表 + 创建(KV 配置)+ 删除(唯一副本守卫由后端兜底)。 */
export function ChannelsPage() {
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm<ChannelForm>();

  const list = useList<ChannelItem>({
    resource: "channels",
    pagination: { currentPage: 1, pageSize: 100 },
  });
  const items = list.result?.data ?? [];
  const refresh = () => invalidate({ resource: "channels", invalidates: ["list"] });

  const create = async (values: ChannelForm) => {
    setSaving(true);
    try {
      const config: Record<string, string> = {};
      for (const kv of values.config ?? []) {
        if (kv?.key) {
          config[kv.key] = kv.value ?? "";
        }
      }
      await apiFetch("/api/admin/channels", {
        method: "POST",
        body: JSON.stringify({
          name: values.name,
          driver: values.driver,
          priority: values.priority,
          encrypted: values.encrypted,
          allow_direct_link: values.allow_direct_link,
          config,
        }),
      });
      message.success("渠道已创建");
      setOpen(false);
      form.resetFields();
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "创建失败");
    } finally {
      setSaving(false);
    }
  };

  const remove = async (id: number) => {
    try {
      await apiFetch(`/api/admin/channels/${id}`, { method: "DELETE" });
      message.success("渠道已删除");
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "删除失败");
    }
  };

  return (
    <Flex vertical gap={16}>
      <Flex align="center" justify="space-between">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            存储渠道
          </Typography.Title>
          <Typography.Text type="secondary">
            多渠道主备容灾;优先级最高的健康渠道承接上传主写
          </Typography.Text>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建渠道
        </Button>
      </Flex>

      <Card styles={{ body: { padding: 0 } }}>
        <Table<ChannelItem>
          rowKey="id"
          dataSource={items}
          loading={list.query.isLoading}
          pagination={false}
          size="middle"
          columns={[
            { title: "渠道", dataIndex: "name" },
            { title: "驱动", dataIndex: "driver", render: (v: string) => <Tag>{v}</Tag> },
            { title: "优先级", dataIndex: "priority", align: "right", width: 90 },
            {
              title: "状态",
              dataIndex: "healthy",
              width: 90,
              render: (v: boolean) =>
                v ? <Tag color="success">健康</Tag> : <Tag color="error">异常</Tag>,
            },
            {
              title: "特性",
              key: "flags",
              render: (_, row) => (
                <Space size={4}>
                  {row.encrypted && <Tag color="geekblue">加密</Tag>}
                  {row.allow_direct_link && <Tag color="cyan">可直链</Tag>}
                </Space>
              ),
            },
            {
              title: "操作",
              key: "actions",
              width: 100,
              render: (_, row) => (
                <Popconfirm
                  title={`删除渠道 ${row.name}?`}
                  description="持有唯一副本的渠道会被拒绝删除,防止丢数据。"
                  okButtonProps={{ danger: true }}
                  onConfirm={() => remove(row.id)}
                >
                  <Button size="small" danger type="text">
                    删除
                  </Button>
                </Popconfirm>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={open}
        title="新建渠道"
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={saving}
        okText="创建"
        width={560}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={create}
          initialValues={{ priority: 100, encrypted: false, allow_direct_link: false }}
        >
          <Flex gap={12}>
            <Form.Item
              name="name"
              label="渠道名"
              rules={[{ required: true, message: "请输入渠道名" }]}
              style={{ flex: 1 }}
            >
              <Input placeholder="r2-backup" />
            </Form.Item>
            <Form.Item
              name="driver"
              label="rclone 驱动"
              rules={[{ required: true, message: "请输入驱动名" }]}
              style={{ flex: 1 }}
            >
              <Input placeholder="s3 / local / webdav …" />
            </Form.Item>
            <Form.Item name="priority" label="优先级" style={{ width: 96 }}>
              <InputNumber min={0} max={1000} style={{ width: "100%" }} />
            </Form.Item>
          </Flex>
          <Flex gap={24}>
            <Form.Item name="encrypted" label="服务端加密" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="allow_direct_link" label="允许 302 直链" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Flex>
          <Form.Item label="后端参数" extra="rclone 后端参数;local 用 path,S3 类用 access_key_id 等">
            <Form.List name="config">
              {(fields, { add, remove: removeRow }) => (
                <Flex vertical gap={8}>
                  {fields.map((field) => (
                    <Flex key={field.key} gap={8} align="baseline">
                      <Form.Item name={[field.name, "key"]} noStyle>
                        <Input placeholder="键(如 path)" style={{ width: 180 }} />
                      </Form.Item>
                      <Form.Item name={[field.name, "value"]} noStyle>
                        <Input placeholder="值" style={{ flex: 1 }} />
                      </Form.Item>
                      <MinusCircleOutlined onClick={() => removeRow(field.name)} />
                    </Flex>
                  ))}
                  <Button type="dashed" onClick={() => add()} icon={<PlusOutlined />} block>
                    添加参数
                  </Button>
                </Flex>
              )}
            </Form.List>
          </Form.Item>
        </Form>
      </Modal>
    </Flex>
  );
}
