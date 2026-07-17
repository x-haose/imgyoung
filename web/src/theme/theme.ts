import { theme as antdTheme, type ThemeConfig } from "antd";

/**
 * 设计令牌:深邃工具感,双主题(暗色默认)。
 * 主色翡翠绿贯穿品牌;暗色以分层近黑面板表达深度,亮色以低饱和灰阶保持同一气质。
 */
export const brand = {
  primary: "#10b981", // 翡翠绿:品牌点缀色,语义为"就绪/健康"
  primaryHover: "#34d399",
  dark: {
    bgBase: "#0b0e11", // 最底层画布
    bgElevated: "#12161b", // 卡片/面板
    bgSpotlight: "#1a2027", // 悬浮层/下拉
    border: "#232a33",
    text: "rgba(235, 241, 245, 0.88)",
    textSecondary: "rgba(235, 241, 245, 0.55)",
  },
  light: {
    bgBase: "#f6f7f9",
    bgElevated: "#ffffff",
    border: "#e3e7eb",
    text: "rgba(18, 24, 29, 0.88)",
    textSecondary: "rgba(18, 24, 29, 0.55)",
  },
} as const;

/** 两种主题共享的组件级令牌。 */
const shared: ThemeConfig["components"] = {
  Layout: { headerHeight: 56 },
  Card: { borderRadiusLG: 12 },
  Button: { controlHeight: 34 },
  Table: { cellPaddingBlock: 12 },
  Menu: { itemBorderRadius: 8, itemMarginInline: 10, iconMarginInlineEnd: 12 },
};

/** 暗色主题(默认):近黑分层 + 翡翠绿点缀。 */
export const darkTheme: ThemeConfig = {
  algorithm: antdTheme.darkAlgorithm,
  token: {
    colorPrimary: brand.primary,
    colorInfo: brand.primary,
    colorBgBase: brand.dark.bgBase,
    colorBgContainer: brand.dark.bgElevated,
    colorBgElevated: brand.dark.bgSpotlight,
    colorBorder: brand.dark.border,
    colorBorderSecondary: "#1c2229",
    colorText: brand.dark.text,
    colorTextSecondary: brand.dark.textSecondary,
    borderRadius: 8,
    fontFamily:
      "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
    fontSize: 13,
  },
  components: {
    ...shared,
    // 侧栏与画布同系分层:面板近黑,不用 antd 内置深蓝。
    Layout: { ...shared?.Layout, siderBg: brand.dark.bgElevated },
    Menu: {
      ...shared?.Menu,
      darkItemBg: "transparent",
      darkSubMenuItemBg: "transparent",
      darkItemSelectedBg: brand.primary,
      darkItemSelectedColor: "#052e22",
    },
  },
};

/** 亮色主题:同一气质的低饱和灰阶。 */
export const lightTheme: ThemeConfig = {
  algorithm: antdTheme.defaultAlgorithm,
  token: {
    colorPrimary: brand.primary,
    colorInfo: brand.primary,
    colorBgBase: brand.light.bgBase,
    colorBgContainer: brand.light.bgElevated,
    colorBorder: brand.light.border,
    colorText: brand.light.text,
    colorTextSecondary: brand.light.textSecondary,
    borderRadius: 8,
    fontFamily:
      "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
    fontSize: 13,
  },
  components: {
    ...shared,
    Layout: { ...shared?.Layout, siderBg: brand.light.bgElevated },
    Menu: { ...shared?.Menu, itemSelectedBg: "#d8f3e8" },
  },
};

/** 主题模式的 localStorage 键。 */
export const themeStorageKey = "imgyoung-theme";

/** 读取持久化的主题模式,默认暗色。 */
export function storedThemeMode(): "dark" | "light" {
  return localStorage.getItem(themeStorageKey) === "light" ? "light" : "dark";
}
