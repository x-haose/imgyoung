import { brand } from "../theme/theme";

/** BrandMark 的属性。 */
interface BrandMarkProps {
  size?: number; // 边长,像素
}

/** 品牌标:翡翠绿圆角方块内嵌快门光圈线条,内联 SVG 零资源依赖。 */
export function BrandMark({ size = 24 }: BrandMarkProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden>
      <rect x="1" y="1" width="30" height="30" rx="8" fill={brand.primary} />
      <circle cx="16" cy="16" r="8.5" fill="none" stroke="#06251b" strokeWidth="2.4" />
      <path
        d="M16 7.5 L16 16 L22.5 21"
        fill="none"
        stroke="#06251b"
        strokeWidth="2.4"
        strokeLinecap="round"
      />
    </svg>
  );
}
