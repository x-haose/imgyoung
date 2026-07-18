import { DownloadOutlined } from "@ant-design/icons";
import { useLogout, usePermissions } from "@refinedev/core";
import { Alert, App as AntdApp, Button, Card, Flex, Form, Input, Typography } from "antd";
import { useState } from "react";

import { apiFetch } from "../../api/http";
import { sessionUser } from "../../providers/authProvider";
import { RefererCard } from "./RefererCard";
import { RuntimeConfigCard } from "./RuntimeConfigCard";

/** 改密表单值。 */
interface PasswordForm {
  old_password: string;
  new_password: string;
  confirm: string;
}

/** 设置页:修改密码(改后全端下线重新登录);管理员另有运行配置、防盗链与元数据导出。 */
export function SettingsPage() {
  const [saving, setSaving] = useState(false);
  const { mutate: logout } = useLogout();
  const { data: role } = usePermissions<string>({});
  const { message } = AntdApp.useApp();
  const user = sessionUser();
  const [form] = Form.useForm<PasswordForm>();

  const changePassword = async (values: PasswordForm) => {
    setSaving(true);
    try {
      await apiFetch("/api/admin/me/password", {
        method: "PUT",
        body: JSON.stringify({
          old_password: values.old_password,
          new_password: values.new_password,
        }),
      });
      message.success("密码已修改,请重新登录");
      logout();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "修改失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Flex vertical gap={16} style={{ maxWidth: 640 }}>
      <div>
        <Typography.Title level={4} style={{ margin: 0 }}>
          设置
        </Typography.Title>
        <Typography.Text type="secondary">当前账号:{user?.name}</Typography.Text>
      </div>

      <Card title="修改密码">
        <Form form={form} layout="vertical" onFinish={changePassword}>
          <Form.Item
            name="old_password"
            label="旧密码"
            rules={[{ required: true, message: "请输入旧密码" }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            name="new_password"
            label="新密码"
            rules={[{ required: true, message: "请输入新密码" }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="confirm"
            label="确认新密码"
            dependencies={["new_password"]}
            rules={[
              { required: true, message: "请再次输入新密码" },
              ({ getFieldValue }) => ({
                validator: (_, value) =>
                  value === getFieldValue("new_password")
                    ? Promise.resolve()
                    : Promise.reject(new Error("两次输入不一致")),
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
            修改后当前登录的全部设备都会下线,需要重新登录。
          </Typography.Paragraph>
          <Button type="primary" htmlType="submit" loading={saving}>
            修改密码
          </Button>
        </Form>
      </Card>

      {role === "admin" && (
        <>
          <RuntimeConfigCard />
          <RefererCard />
          <Card title="数据导出">
            <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
              导出全部元数据(用户、渠道、图片、副本分布,凭证脱敏)。图与账本都在渠道里,
              即便项目停更也能凭此自救。
            </Typography.Paragraph>
            <Alert
              type="warning"
              showIcon
              message="主密钥文件须单独离线备份"
              description="主密钥(数据目录下的 master.key)不包含在数据导出与远端快照中;
              丢失后渠道凭证与加密渠道数据将永久不可恢复,请复制该文件到独立的安全位置。"
              style={{ marginBottom: 16 }}
            />
            <Button icon={<DownloadOutlined />} href="/api/admin/export" target="_blank">
              导出 JSON
            </Button>
          </Card>
        </>
      )}
    </Flex>
  );
}
