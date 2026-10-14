import { cn } from '@/lib/utils'

/** 品牌标:翡翠绿圆角方块内嵌快门光圈线条,内联 SVG 零资源依赖。 */
export function BrandMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox='0 0 32 32'
      role='img'
      aria-label='ImgYoung'
      className={cn('shrink-0', className)}
    >
      <rect x='1' y='1' width='30' height='30' rx='8' fill='#10b981' />
      <circle
        cx='16'
        cy='16'
        r='8.5'
        fill='none'
        stroke='#06251b'
        strokeWidth='2.4'
      />
      <path
        d='M16 7.5 L16 16 L22.5 21'
        fill='none'
        stroke='#06251b'
        strokeWidth='2.4'
        strokeLinecap='round'
      />
    </svg>
  )
}
