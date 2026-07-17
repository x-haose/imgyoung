import { useLogin } from "@refinedev/core";
import { Alert, Button, Card, Flex, Form, Input, Typography, theme } from "antd";
import { useState } from "react";

import { BrandMark } from "../../layout/BrandMark";

/** 登录表单字段。 */
interface LoginForm {
  name: string;
  password: string;
}

/** 登录页:画布中央的单卡片,品牌标 + 表单,错误就地提示。 */
export function LoginPage() {
  const { mutate: login, isPending } = useLogin<LoginForm>();
  const [error, setError] = useState("");
  const { token } = theme.useToken();

  const onFinish = (values: LoginForm) => {
    setError("");
    login(values, {
      onSuccess: (result) => {
        if (!result.success) {
          setError(result.error?.message ?? "登录失败");
        }
      },
    });
  };

  return (
    <Flex
      align="center"
      justify="center"
      style={{
        minHeight: "100vh",
        background: token.colorBgLayout,
        // 画布上方一抹品牌色光晕,给"深邃工具感"以纵深,零图片资源。
        backgroundImage:
          "radial-gradient(600px 320px at 50% 18%, rgba(16, 185, 129, 0.14), transparent 70%)",
      }}
    >
      <Card style={{ width: 360, boxShadow: token.boxShadowSecondary }}>
        <Flex vertical align="center" gap={6} style={{ marginBottom: 24, marginTop: 8 }}>
          <BrandMark size={40} />
          <Typography.Title level={4} style={{ margin: "10px 0 0" }}>
            ImgYoung 管理后台
          </Typography.Title>
          <Typography.Text type="secondary">自部署多渠道图床</Typography.Text>
        </Flex>
        {error !== "" && (
          <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />
        )}
        <Form<LoginForm> layout="vertical" onFinish={onFinish} requiredMark={false}>
          <Form.Item
            name="name"
            label="用户名"
            rules={[{ required: true, message: "请输入用户名" }]}
          >
            <Input autoComplete="username" autoFocus />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[{ required: true, message: "请输入密码" }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={isPending} size="large">
            登录
          </Button>
        </Form>
      </Card>
    </Flex>
  );
}
