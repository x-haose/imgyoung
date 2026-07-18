/** 展示层格式化工具:字节、时间、链接。 */

/**
 * 把服务端返回的图片访问路径补全为可分享的绝对链接。
 * 相对路径以当前页面 origin 补全;配置了 base_url 的绝对地址原样保留。
 */
export function absoluteURL(path: string): string {
  return new URL(path, window.location.origin).href;
}

/** 把字节格式化为人类可读单位;空值显示为占位符。 */
export function formatBytes(n: number | undefined | null): string {
  if (n === undefined || n === null) {
    return "—";
  }
  if (n < 1024) {
    return `${n} B`;
  }
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

/** 把本地时刻(毫秒时间戳)转为后端可比较的 UTC 文本时间(YYYY-MM-DD HH:MM:SS)。 */
export function toUTCText(ms: number): string {
  return new Date(ms).toISOString().slice(0, 19).replace("T", " ");
}

/** 把后端 UTC 文本时间转为「N 秒/分钟/小时/天前」的相对描述;空值或不可解析返回空串。 */
export function formatRelativeTime(utc: string | null | undefined): string {
  if (!utc) {
    return "";
  }
  const d = new Date(utc.replace(" ", "T") + "Z");
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  const seconds = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
  if (seconds < 60) {
    return `${seconds} 秒前`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes} 分钟前`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return `${hours} 小时前`;
  }
  return `${Math.floor(hours / 24)} 天前`;
}

/** 把后端 UTC 文本时间(YYYY-MM-DD HH:MM:SS)转为本地时间展示;空值显示占位符。 */
export function formatTime(utc: string | null | undefined): string {
  if (!utc) {
    return "—";
  }
  const d = new Date(utc.replace(" ", "T") + "Z");
  if (Number.isNaN(d.getTime())) {
    return utc;
  }
  return d.toLocaleString("zh-CN", { hour12: false });
}
