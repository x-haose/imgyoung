import { useEffect, useRef } from 'react'
import { cn } from '@/lib/utils'
import { startAurora } from './animate'

/**
 * 未登录页面的极光背景:品牌色光带集中在画面上部,开场流动数秒后静止,随指针轻微视差。
 * 纯装饰,对读屏隐藏;不支持 WebGL2 时画布透明,由父容器的静态 CSS 背景兜底。
 */
export function AuroraBackground({ className }: { className?: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    return startAurora(canvas)
  }, [])

  return (
    <canvas
      ref={canvasRef}
      aria-hidden='true'
      className={cn(
        'pointer-events-none absolute inset-0 size-full',
        className
      )}
    />
  )
}
