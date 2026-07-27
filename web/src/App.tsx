import {
  AuditOutlined,
  BookOutlined,
  ClearOutlined,
  ClusterOutlined,
  DashboardOutlined,
  DiffOutlined,
  KeyOutlined,
  PictureOutlined,
  SettingOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { useNotificationProvider } from "@refinedev/antd";
import { Authenticated, Refine, type I18nProvider } from "@refinedev/core";
import routerProvider, { CatchAllNavigate, NavigateToResource } from "@refinedev/react-router";
import { App as AntdApp, ConfigProvider } from "antd";
import zhCNLocale from "antd/locale/zh_CN";
import { type ReactNode, useMemo, useState } from "react";
import { BrowserRouter, Navigate, Outlet, Route, Routes } from "react-router";

import { interpolate, zhCN } from "./i18n/zhCN";
import { AppLayout } from "./layout/AppLayout";
import { AuditPage } from "./pages/audit";
import { ChannelsPage } from "./pages/channels";
import { CleanupPage } from "./pages/cleanup";
import { DashboardPage } from "./pages/dashboard";
import { DedupPage } from "./pages/dedup";
import { FirstRunPasswordPage } from "./pages/first-run-password";
import { GuidePage } from "./pages/guide";
import { ImagesPage } from "./pages/images";
import { LoginPage } from "./pages/login";
import { NotFoundPage } from "./pages/not-found";
import { SettingsPage } from "./pages/settings";
import { TokensPage } from "./pages/tokens";
import { UsersPage } from "./pages/users";
import { authProvider, FIRST_RUN_PATH, sessionUser } from "./providers/authProvider";
import { dataProvider } from "./providers/dataProvider";
import { darkTheme, lightTheme, storedThemeMode, themeStorageKey } from "./theme/theme";

/** i18nProvider:零依赖的 zh-CN 词典,未覆盖键回落 defaultMessage。 */
const i18nProvider: I18nProvider = {
  translate: (key, params, defaultMessage) => {
    const hit = zhCN[key];
    if (hit !== undefined) {
      return interpolate(hit, params as Record<string, unknown> | undefined);
    }
    return defaultMessage ?? key;
  },
  changeLocale: async () => undefined,
  getLocale: () => "zh-CN",
};

/** PasswordGate 首次改密守卫:仍用出厂默认密码时,把业务页一律重定向到首次改密页。 */
function PasswordGate({ children }: { children: ReactNode }) {
  if (sessionUser()?.must_change_password) {
    return <Navigate to={FIRST_RUN_PATH} replace />;
  }
  return <>{children}</>;
}

/** App 根:主题上下文 + Refine 装配 + 路由。 */
export function App() {
  const [mode, setMode] = useState<"dark" | "light">(storedThemeMode);
  const themeConfig = useMemo(() => (mode === "dark" ? darkTheme : lightTheme), [mode]);
  const changeMode = (next: "dark" | "light") => {
    localStorage.setItem(themeStorageKey, next);
    setMode(next);
  };

  return (
    <ConfigProvider theme={themeConfig} locale={zhCNLocale}>
      <AntdApp>
        <BrowserRouter>
          <Refine
            dataProvider={dataProvider}
            authProvider={authProvider}
            routerProvider={routerProvider}
            i18nProvider={i18nProvider}
            notificationProvider={useNotificationProvider}
            options={{ syncWithLocation: true, warnWhenUnsavedChanges: true, disableTelemetry: true }}
            resources={[
              {
                name: "dashboard",
                list: "/",
                meta: { label: "总览", icon: <DashboardOutlined /> },
              },
              {
                name: "images",
                list: "/images",
                meta: { label: "图片", icon: <PictureOutlined /> },
              },
              {
                name: "tokens",
                list: "/tokens",
                meta: { label: "上传 Token", icon: <KeyOutlined /> },
              },
              {
                name: "channels",
                list: "/channels",
                meta: { label: "存储渠道", icon: <ClusterOutlined />, adminOnly: true },
              },
              {
                name: "users",
                list: "/users",
                meta: { label: "用户", icon: <TeamOutlined />, adminOnly: true },
              },
              {
                name: "dedup",
                list: "/dedup",
                meta: { label: "疑似重复", icon: <DiffOutlined />, adminOnly: true },
              },
              {
                name: "cleanup",
                list: "/cleanup",
                meta: { label: "待清理", icon: <ClearOutlined />, adminOnly: true },
              },
              {
                name: "audit",
                list: "/audit",
                meta: { label: "审计日志", icon: <AuditOutlined />, adminOnly: true },
              },
              {
                name: "guide",
                list: "/guide",
                meta: { label: "使用指南", icon: <BookOutlined /> },
              },
              {
                name: "settings",
                list: "/settings",
                meta: { label: "设置", icon: <SettingOutlined /> },
              },
            ]}
          >
            <Routes>
              <Route
                element={
                  <Authenticated key="app" fallback={<CatchAllNavigate to="/login" />}>
                    <Outlet />
                  </Authenticated>
                }
              >
                {/* 首次改密页:已登录即可访问,不套后台框架、不过改密守卫,避免重定向自循环 */}
                <Route path={FIRST_RUN_PATH} element={<FirstRunPasswordPage />} />
                <Route
                  element={
                    <PasswordGate>
                      <AppLayout mode={mode} onModeChange={changeMode}>
                        <Outlet />
                      </AppLayout>
                    </PasswordGate>
                  }
                >
                  <Route index element={<DashboardPage />} />
                  <Route path="/images" element={<ImagesPage />} />
                  <Route path="/tokens" element={<TokensPage />} />
                  <Route path="/channels" element={<ChannelsPage />} />
                  <Route path="/users" element={<UsersPage />} />
                  <Route path="/dedup" element={<DedupPage />} />
                  <Route path="/cleanup" element={<CleanupPage />} />
                  <Route path="/audit" element={<AuditPage />} />
                  <Route path="/guide" element={<GuidePage />} />
                  <Route path="/settings" element={<SettingsPage />} />
                  <Route path="*" element={<NotFoundPage />} />
                </Route>
              </Route>
              <Route
                element={
                  <Authenticated key="auth" fallback={<Outlet />}>
                    <NavigateToResource resource="dashboard" />
                  </Authenticated>
                }
              >
                <Route path="/login" element={<LoginPage />} />
              </Route>
            </Routes>
          </Refine>
        </BrowserRouter>
      </AntdApp>
    </ConfigProvider>
  );
}
