import { PlusOutlined } from "@ant-design/icons";
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
  Progress,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  Select,
} from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { formatBytes, formatTime } from "../../lib/format";

/** 用户列表项(对应 /api/admin/users 响应)。 */
interface UserItem {
  id: string;
  name: string;
  role: "admin" | "user";
  quota_bytes: number | null;
  disabled: boolean;
  created_at: string;
  used_bytes: number;
  image_count: number;
}

/** 建号/编辑表单值;配额以 GB 输入,0 表示不限。 */
interface UserForm {
  name: string;
  password?: string;
  role: "admin" | "user";
  quota_gb: number;
  disabled?: boolean;
}

/** 字节换算:GB 输入 → 字节。 */
const bytesPerGB = 1 << 30;

/** 用户页:列表(含用量)+ 建号 + 编辑 + 重置密码 + 删除。 */
export function UsersPage() {
  const [editing, setEditing] = useState<UserItem | "new" | null>(null);
  const [resetFor, setResetFor] = useState<UserItem | null>(null);
  const [saving, setSaving] = useState(false);
  const invalidate = useInvalidate();
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm<UserForm>();
  const [resetForm] = Form.useForm<{ password: string }>();

  const list = useList<UserItem>({
    resource: "users",
    pagination: { currentPage: 1, pageSize: 200 },
  });
  const items = list.result?.data ?? [];
  const refresh = () => invalidate({ resource: "users", invalidates: ["list"] });

  const openEditor = (target: UserItem | "new") => {
    setEditing(target);
    if (target === "new") {
      form.setFieldsValue({ name: "", password: "", role: "user", quota_gb: 0, disabled: false });
    } else {
      form.setFieldsValue({
        name: target.name,
        role: target.role,
        quota_gb: target.quota_bytes ? Math.round(target.quota_bytes / bytesPerGB) : 0,
        disabled: target.disabled,
      });
    }
  };

  const save = async (values: UserForm) => {
    setSaving(true);
    try {
      const quotaBytes = values.quota_gb > 0 ? values.quota_gb * bytesPerGB : 0;
      if (editing === "new") {
        await apiFetch("/api/admin/users", {
          method: "POST",
          body: JSON.stringify({
            name: values.name,
            password: values.password,
            role: values.role,
            quota_bytes: quotaBytes,
          }),
        });
        message.success("用户已创建");
      } else if (editing) {
        await apiFetch(`/api/admin/users/${editing.id}`, {
          method: "PUT",
          body: JSON.stringify({
            name: values.name,
            role: values.role,
            quota_bytes: quotaBytes,
            disabled: values.disabled ?? false,
          }),
        });
        message.success("已保存");
      }
      setEditing(null);
      refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const resetPassword = async (values: { password: string }) => {
    if (!resetFor) {
      return;
    }
    setSaving(true);
    try {
      await apiFetch(`/api/admin/users/${resetFor.id}/password`, {
        method: "POST",
        body: JSON.stringify(values),
      });
      message.success(`已重置 ${resetFor.name} 的密码`);
      setResetFor(null);
      resetForm.resetFields();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "重置失败");
    } finally {
      setSaving(false);
    }
  };

  const remove = async (row: UserItem) => {
    try {
      await apiFetch(`/api/admin/users/${row.id}`, { method: "DELETE" });
      message.success("用户已删除");
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
            用户
          </Typography.Title>
          <Typography.Text type="secondary">
            管理员建号,不开放注册;配额按引用计全额
          </Typography.Text>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor("new")}>
          建号
        </Button>
      </Flex>

      <Card styles={{ body: { padding: 0 } }}>
        <Table<UserItem>
          rowKey="id"
          dataSource={items}
          loading={list.query.isLoading}
          pagination={false}
          size="middle"
          columns={[
            {
              title: "用户",
              key: "name",
              render: (_, row) => (
                <Space>
                  <Typography.Text strong>{row.name}</Typography.Text>
                  {row.role === "admin" && <Tag color="gold">管理员</Tag>}
                  {row.disabled && <Tag>已禁用</Tag>}
                </Space>
              ),
            },
            { title: "图片", dataIndex: "image_count", align: "right", width: 80 },
            {
              title: "用量 / 配额",
              key: "usage",
              width: 260,
              render: (_, row) => {
                if (!row.quota_bytes) {
                  return (
                    <Typography.Text type="secondary">
                      {formatBytes(row.used_bytes)} / 不限
                    </Typography.Text>
                  );
                }
                const percent = Math.min(100, Math.round((row.used_bytes / row.quota_bytes) * 100));
                return (
                  <Flex vertical gap={2}>
                    <Progress
                      percent={percent}
                      size="small"
                      status={percent >= 90 ? "exception" : "normal"}
                      showInfo={false}
                    />
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {formatBytes(row.used_bytes)} / {formatBytes(row.quota_bytes)}
                    </Typography.Text>
                  </Flex>
                );
              },
            },
            { title: "创建时间", dataIndex: "created_at", render: formatTime },
            {
              title: "操作",
              key: "actions",
              width: 200,
              render: (_, row) => (
                <Space size={0}>
                  <Button size="small" type="text" onClick={() => openEditor(row)}>
                    编辑
                  </Button>
                  <Button size="small" type="text" onClick={() => setResetFor(row)}>
                    重置密码
                  </Button>
                  <Popconfirm
                    title={`删除用户 ${row.name}?`}
                    description="名下仍有图片时会被拒绝;Token 与会话随删除失效。"
                    okButtonProps={{ danger: true }}
                    onConfirm={() => remove(row)}
                  >
                    <Button size="small" type="text" danger>
                      删除
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={editing !== null}
        title={editing === "new" ? "建号" : `编辑 ${editing?.name ?? ""}`}
        onCancel={() => setEditing(null)}
        onOk={() => form.submit()}
        confirmLoading={saving}
        okText="保存"
      >
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item
            name="name"
            label="登录名"
            rules={[{ required: true, message: "请输入登录名" }]}
          >
            <Input />
          </Form.Item>
          {editing === "new" && (
            <Form.Item
              name="password"
              label="初始密码"
              rules={[{ required: true, message: "请输入初始密码" }]}
            >
              <Input.Password autoComplete="new-password" />
            </Form.Item>
          )}
          <Flex gap={12}>
            <Form.Item name="role" label="角色" style={{ flex: 1 }}>
              <Select
                options={[
                  { value: "user", label: "普通用户" },
                  { value: "admin", label: "管理员" },
                ]}
              />
            </Form.Item>
            <Form.Item name="quota_gb" label="配额(GB,0 = 不限)" style={{ flex: 1 }}>
              <InputNumber min={0} max={100000} style={{ width: "100%" }} />
            </Form.Item>
          </Flex>
          {editing !== "new" && (
            <Form.Item name="disabled" label="禁用(即时吊销会话)" valuePropName="checked">
              <Switch />
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        open={resetFor !== null}
        title={`重置 ${resetFor?.name ?? ""} 的密码`}
        onCancel={() => setResetFor(null)}
        onOk={() => resetForm.submit()}
        confirmLoading={saving}
        okText="重置"
      >
        <Form form={resetForm} layout="vertical" onFinish={resetPassword}>
          <Form.Item
            name="password"
            label="新密码"
            rules={[{ required: true, message: "请输入新密码" }]}
            extra="重置后该用户的全部会话即刻失效"
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Flex>
  );
}
