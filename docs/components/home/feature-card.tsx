'use client'

import type { PointerEvent, ReactNode } from 'react'

/**
 * 功能卡片:指针在卡片上移动时,一团品牌色光斑跟随指针(只改 CSS 变量,不触发重渲染);
 * 只响应指针悬停,不自动播放。
 */
export function FeatureCard({ title, children }: { title: string; children: ReactNode }) {
  const onPointerMove = (e: PointerEvent<HTMLLIElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    e.currentTarget.style.setProperty('--spot-x', `${e.clientX - rect.left}px`)
    e.currentTarget.style.setProperty('--spot-y', `${e.clientY - rect.top}px`)
  }
  return (
    <li
      onPointerMove={onPointerMove}
      className='group relative overflow-hidden rounded-xl border border-fd-border bg-fd-card p-6 transition-colors hover:border-[color-mix(in_oklab,var(--color-brand)_45%,var(--color-fd-border))]'
    >
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0 opacity-0 transition-opacity duration-300 group-hover:opacity-100'
        style={{
          background:
            'radial-gradient(260px circle at var(--spot-x, 50%) var(--spot-y, 50%), color-mix(in oklab, var(--color-brand) 14%, transparent), transparent 70%)',
        }}
      />
      <h3 className='relative text-base font-semibold'>{title}</h3>
      <p className='relative mt-2 text-sm leading-relaxed text-fd-muted-foreground'>{children}</p>
    </li>
  )
}
