'use client'

import { useState } from 'react'

/** 示意图文案。 */
export type FailoverDemoText = {
  visitor: string // 访问者节点标签
  primary: string // 主渠道节点标签
  backups: [string, string] // 两个备份渠道节点标签
  down: string // 故障状态标签
  serving: string // 正在服务的标记
  fail: string // 「模拟主渠道故障」按钮
  recover: string // 「恢复主渠道」按钮
  healthyCaption: string // 正常时的说明
  failedCaption: string // 故障时的说明
}

/** 渠道节点的纵向位置(SVG 坐标),顺序即优先级。 */
const CHANNEL_Y = [60, 150, 240]

/** 节点宽度(SVG 坐标)。 */
const NODE_W = 110

/**
 * 首页主视觉:访问者 → ImgYoung → 三个按优先级排列的渠道。
 * 点按钮模拟主渠道故障,读取路径即时切到下一个有副本的渠道。动效只在切换后流动三次,
 * 不自动循环;减少动效偏好下不流动。
 */
export function FailoverDemo({ text }: { text: FailoverDemoText }) {
  const [failed, setFailed] = useState(false)
  const serving = failed ? 1 : 0
  const labels = [text.primary, ...text.backups]

  return (
    <figure className='flex w-full max-w-xl flex-col gap-4'>
      <svg
        viewBox='0 0 580 300'
        role='img'
        aria-label={failed ? text.failedCaption : text.healthyCaption}
        className='w-full overflow-visible'
      >
        <Node x={0} y={150} label={text.visitor} tone='muted' />
        <Link from={[NODE_W, 150]} to={[190, 150]} active flowKey={serving} />
        <Node x={190} y={150} label='ImgYoung' tone='brand' />
        {CHANNEL_Y.map((y, i) => (
          <Link
            key={`l${y}`}
            from={[190 + NODE_W, 150]}
            to={[380, y]}
            active={i === serving}
            flowKey={serving}
          />
        ))}
        {CHANNEL_Y.map((y, i) => (
          <Node
            key={`n${y}`}
            x={380}
            y={y}
            label={labels[i]}
            tone={i === 0 && failed ? 'down' : i === serving ? 'active' : 'idle'}
            badge={i === 0 && failed ? text.down : i === serving ? text.serving : undefined}
          />
        ))}
      </svg>
      <figcaption className='min-h-12 text-sm leading-relaxed text-fd-muted-foreground'>
        {failed ? text.failedCaption : text.healthyCaption}
      </figcaption>
      <button
        type='button'
        onClick={() => setFailed((v) => !v)}
        className='self-start rounded-lg border border-fd-border bg-fd-card px-3.5 py-2 text-sm font-medium transition-colors hover:bg-fd-accent focus-visible:ring-2 focus-visible:ring-fd-ring focus-visible:outline-none'
      >
        {failed ? text.recover : text.fail}
      </button>
    </figure>
  )
}

type Tone = 'brand' | 'active' | 'idle' | 'down' | 'muted'

/** 各状态的描边与填充,取文档站主题变量,亮暗主题自动适配。 */
const TONE: Record<Tone, { stroke: string; fill: string; text: string }> = {
  brand: {
    stroke: 'var(--color-brand)',
    fill: 'var(--color-brand)',
    text: 'var(--color-brand-ink)',
  },
  active: {
    stroke: 'var(--color-brand)',
    fill: 'color-mix(in oklab, var(--color-brand) 14%, var(--color-fd-card))',
    text: 'var(--color-fd-foreground)',
  },
  idle: {
    stroke: 'var(--color-fd-border)',
    fill: 'var(--color-fd-card)',
    text: 'var(--color-fd-foreground)',
  },
  down: {
    stroke: 'var(--color-danger)',
    fill: 'color-mix(in oklab, var(--color-danger) 12%, var(--color-fd-card))',
    text: 'var(--color-fd-muted-foreground)',
  },
  muted: {
    stroke: 'var(--color-fd-border)',
    fill: 'var(--color-fd-muted)',
    text: 'var(--color-fd-foreground)',
  },
}

/** 一个圆角矩形节点;badge 显示在节点右上方。 */
function Node({
  x,
  y,
  label,
  tone,
  badge,
}: {
  x: number
  y: number
  label: string
  tone: Tone
  badge?: string
}) {
  const t = TONE[tone]
  return (
    <g className='transition-opacity duration-300'>
      <rect
        x={x}
        y={y - 22}
        width={NODE_W}
        height={44}
        rx={10}
        fill={t.fill}
        stroke={t.stroke}
        strokeWidth={1.5}
        className='transition-[fill,stroke] duration-300'
      />
      <text
        x={x + NODE_W / 2}
        y={y + 4.5}
        textAnchor='middle'
        fontSize={12.5}
        fontWeight={600}
        fill={t.text}
        className={tone === 'down' ? 'line-through' : undefined}
      >
        {label}
      </text>
      {badge && (
        <text x={x + NODE_W + 10} y={y + 4} fontSize={11} fill={t.stroke} fontWeight={600}>
          {badge}
        </text>
      )}
    </g>
  )
}

/**
 * 两个节点间的连线:激活时为品牌色并沿方向流动三次(切换状态时经 key 重播),未激活时为虚线。
 * @param flowKey 状态变化时改变它,让流动动画重新播放
 */
function Link({
  from,
  to,
  active,
  flowKey,
}: {
  from: [number, number]
  to: [number, number]
  active: boolean
  flowKey: number
}) {
  const d = `M${from[0]} ${from[1]} C${from[0] + 40} ${from[1]}, ${to[0] - 40} ${to[1]}, ${to[0]} ${to[1]}`
  return (
    <path
      key={active ? `on-${flowKey}` : 'off'}
      d={d}
      fill='none'
      stroke={active ? 'var(--color-brand)' : 'var(--color-fd-border)'}
      strokeWidth={active ? 2 : 1.5}
      strokeDasharray={active ? '6 6' : '3 5'}
      className={active ? 'motion-safe:animate-[failover-flow_1s_linear_3]' : undefined}
    />
  )
}
