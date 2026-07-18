/** 剪贴板工具:统一复制入口,调用方只关心成败提示。 */

/**
 * 把文本写入剪贴板;失败(非安全上下文/权限拒绝)返回 false,由调用方提示。
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
