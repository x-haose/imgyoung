import { LogoutOutlined, MoonOutlined, SunOutlined } from "@ant-design/icons";
import { useLogout, useMenu, usePermissions } from "@refinedev/core";
import { Button, Dropdown, Flex, Layout, Menu, Tooltip, Typography, theme } from "antd";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { sessionUser } from "../providers/authProvider";
import { BrandMark } from "./BrandMark";

/** AppLayout 的属性。 */
interface AppLayoutProps {
  mode: "dark" | "light"; // 当前主题模式
  onModeChange: (next: "dark" | "light") => void; // 切换主题
  children: ReactNode; // 页面内容
}

/** 后台整体布局:窄侧栏导航 + 顶栏(主题切换/用户)+ 内容区。 */
export function AppLayout({ mode, onModeChange, children }: AppLayoutProps) {
  const { menuItems, selectedKey } = useMenu();
  const { mutate: logout } = useLogout();
  const { data: role } = usePermissions<string>({});
  const { token } = theme.useToken();
  const user = sessionUser();

  const items = menuItems
    .filter((item) => !(item.meta?.adminOnly && role !== "admin"))
    .map((item) => ({
      key: item.key,
      icon: item.meta?.icon,
      label: <Link to={item.route ?? "/"}>{item.label}</Link>,
    }));

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Layout.Sider
        width={208}
        style={{ borderRight: `1px solid ${token.colorBorderSecondary}` }}
        theme={mode}
      >
        <Flex align="center" gap={10} style={{ padding: "18px 20px 14px" }}>
          <BrandMark size={26} />
          <Typography.Text strong style={{ fontSize: 15, letterSpacing: 0.3 }}>
            ImgYoung
          </Typography.Text>
        </Flex>
        <Menu
          mode="inline"
          theme={mode}
          items={items}
          selectedKeys={selectedKey ? [selectedKey] : []}
          style={{ borderInlineEnd: "none", background: "transparent" }}
        />
      </Layout.Sider>
      <Layout>
        <Layout.Header
          style={{
            background: "transparent",
            padding: "0 24px",
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-end",
            gap: 8,
            borderBottom: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          <Tooltip title={mode === "dark" ? "切换亮色" : "切换暗色"}>
            <Button
              type="text"
              icon={mode === "dark" ? <SunOutlined /> : <MoonOutlined />}
              onClick={() => onModeChange(mode === "dark" ? "light" : "dark")}
            />
          </Tooltip>
          <Dropdown
            menu={{
              items: [
                {
                  key: "logout",
                  icon: <LogoutOutlined />,
                  label: "退出登录",
                  onClick: () => logout(),
                },
              ],
            }}
          >
            <Button type="text">{user?.name ?? "…"}</Button>
          </Dropdown>
        </Layout.Header>
        <Layout.Content style={{ padding: 24 }}>{children}</Layout.Content>
      </Layout>
    </Layout>
  );
}
