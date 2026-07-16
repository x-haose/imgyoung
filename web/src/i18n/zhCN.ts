/**
 * Refine 组件文案的 zh-CN 覆盖:官方无中文包,按文档键名自备;
 * 未覆盖的键回落到调用方 defaultMessage。
 */
export const zhCN: Record<string, string> = {
  "pages.login.title": "登录 ImgYoung",
  "pages.login.signin": "登录",
  "buttons.create": "新建",
  "buttons.save": "保存",
  "buttons.logout": "退出登录",
  "buttons.delete": "删除",
  "buttons.edit": "编辑",
  "buttons.cancel": "取消",
  "buttons.confirm": "确认",
  "buttons.filter": "筛选",
  "buttons.clear": "清除",
  "buttons.refresh": "刷新",
  "buttons.show": "查看",
  "notifications.success": "操作成功",
  "notifications.error": "操作失败(状态码 {{statusCode}})",
  "notifications.undoable": "你有 {{seconds}} 秒可以撤销",
  "notifications.createSuccess": "创建成功",
  "notifications.createError": "创建失败(状态码 {{statusCode}})",
  "notifications.deleteSuccess": "删除成功",
  "notifications.deleteError": "删除失败(状态码 {{statusCode}})",
  "notifications.editSuccess": "保存成功",
  "notifications.editError": "保存失败(状态码 {{statusCode}})",
  "table.actions": "操作",
};

/** 简易插值:把 {{key}} 替换为参数值。 */
export function interpolate(template: string, params?: Record<string, unknown>): string {
  if (!params) {
    return template;
  }
  return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => String(params[key] ?? ""));
}
