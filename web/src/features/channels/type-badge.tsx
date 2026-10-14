import { cn } from '@/lib/utils'
import { CUSTOM } from './types'

/** 各存储类型的辨识徽标:短标 + 品牌色,纯 CSS 渲染,不引外部图(守 CSP 与体积预算)。 */
const TYPE_BADGES: Record<string, { text: string; color: string }> = {
  'aws-s3': { text: 'S3', color: '#FF9900' },
  r2: { text: 'R2', color: '#5B6EF5' },
  cos: { text: 'COS', color: '#00A4FF' },
  oss: { text: 'OSS', color: '#FF6A00' },
  b2: { text: 'B2', color: '#E21E29' },
  minio: { text: 'M', color: '#C7264B' },
  's3-compatible': { text: 'S3', color: '#8C8C8C' },
  webdav: { text: 'DAV', color: '#0082C9' },
  onedrive: { text: 'OD', color: '#0078D4' },
  googledrive: { text: 'GD', color: '#1A73E8' },
  dropbox: { text: 'DB', color: '#0061FF' },
  pcloud: { text: 'PC', color: '#00A3E0' },
  box: { text: 'BOX', color: '#0061D5' },
  yandex: { text: 'YA', color: '#FC3F1D' },
  // 列表按驱动名取徽标:s3 系与 Google Drive 的驱动名与模板 id 不同。
  s3: { text: 'S3', color: '#8C8C8C' },
  drive: { text: 'GD', color: '#1A73E8' },
  local: { text: 'HD', color: '#10B981' },
  [CUSTOM]: { text: '⚙', color: '#8C8C8C' },
}

/** 未登记的模板 id 用中性灰 S3 标。 */
const FALLBACK_BADGE = { text: 'S3', color: '#8C8C8C' }

type TypeBadgeProps = {
  id: string // 模板 id;自定义渠道传驱动名
  className?: string // 控制边长,默认 size-9
}

/** 存储类型的品牌徽标方块;三字符短标缩小字号保持居中。 */
export function TypeBadge({ id, className }: TypeBadgeProps) {
  const badge = TYPE_BADGES[id] ?? FALLBACK_BADGE
  const dense = badge.text.length >= 3
  return (
    <div
      aria-hidden='true'
      className={cn(
        'flex size-9 shrink-0 items-center justify-center rounded-lg font-bold',
        dense ? 'text-[0.6rem] tracking-normal' : 'text-sm tracking-wide',
        className
      )}
      style={{ color: badge.color, background: `${badge.color}1F` }}
    >
      {badge.text}
    </div>
  )
}
