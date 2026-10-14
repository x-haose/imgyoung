/** 设置页分区,也是搜索参数 tab 的取值;单独成文件,路由定义引用它时不把整个页面拉进首屏入口块。 */
export const SETTINGS_TABS = ['config', 'referer', 'backup', 'alerts'] as const

export type SettingsTab = (typeof SETTINGS_TABS)[number]
