import { useLogout } from "@refinedev/core";
import { Alert, App as AntdApp, Button, Card, Flex, Form, Input, Typography, theme } from "antd";
import { useState } from "react";
import { Navigate } from "react-router";

import { apiFetch } from "../../api/http";
import { BrandMark } from "../../layout/BrandMark";
import { passwordRules } from "../../lib/password";
import { sessionUser } from "../../providers/authProvider";

/** 出厂默认密码;与后端 auth.DefaultAdminPassword 一致,公开值,仅此页作旧密码提交。 */
const DEFAULT_ADMIN_PASSWORD = "imgyoung123";

/** 首次改密表单字段。 */
interface FirstRunForm {
  new_password: string;
  confirm: string;
}

/**
 * FirstRunPasswordPage 首次强制改密页:仍用出厂默认密码时,已认证用户被路由守卫重定向到此,
 * 改掉默认密码前不放行其余页面。改成功后全端下线,须用新密码重新登录。
 */
export function FirstRunPasswordPage() {
  const user = sessionUser();
  const { mutate: logout } = useLogout();
  const { message } = AntdApp.useApp();
  const { token } = theme.useToken();
  const [saving, setSaving] = useState(false);

  // 已非默认密码者不应停留在本页(如手动输入 URL),回落总览。
  if (!user?.must_change_password) {
    return <Navigate to="/" replace />;
  }

  const submit = async (values: FirstRunForm) => {
    setSaving(true);
    try {
      await apiFetch("/api/admin/me/password", {
        method: "PUT",
        body: JSON.stringify({
          old_password: DEFAULT_ADMIN_PASSWORD,
          new_password: values.new_password,
        }),
      });
      message.success("密码已设置,请用新密码重新登录");
      logout();
    } catch (err) {
      message.error(err instanceof Error ? err.message : "设置失败,请重试");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Flex
      align="center"
      justify="center"
      style={{
        minHeight: "100vh",
        background: token.colorBgLayout,
        backgroundImage:
          "radial-gradient(600px 320px at 50% 18%, rgba(16, 185, 129, 0.14), transparent 70%)",
      }}
    >
      <Card style={{ width: 400, boxShadow: token.boxShadowSecondary }}>
        <Flex vertical align="center" gap={6} style={{ marginBottom: 20, marginTop: 8 }}>
          <BrandMark size={40} />
          <Typography.Title level={4} style={{ margin: "10px 0 0" }}>
            设置管理员密码
          </Typography.Title>
          <Typography.Text type="secondary">首次登录 · 账号 {user.name}</Typography.Text>
        </Flex>
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message="当前使用出厂默认密码"
          description="默认密码是公开的,任何人都能凭它登入。请立即改为只有你知道的密码。"
        />
        <Form<FirstRunForm> layout="vertical" onFinish={submit} requiredMark={false}>
          <Form.Item
            name="new_password"
            label="新密码"
            rules={passwordRules("请输入新密码")}
          >
            <Input.Password autoComplete="new-password" autoFocus />
          </Form.Item>
          <Form.Item
            name="confirm"
            label="确认新密码"
            dependencies={["new_password"]}
            rules={[
              { required: true, message: "请再次输入新密码" },
              ({ getFieldValue }) => ({
                validator: (_, value) =>
                  !value || getFieldValue("new_password") === value
                    ? Promise.resolve()
                    : Promise.reject(new Error("两次输入不一致")),
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={saving} size="large">
            保存并重新登录
          </Button>
        </Form>
      </Card>
    </Flex>
  );
}
