import { useMessages } from '@/context/locale-provider'
import { TypeBadge } from './type-badge'
import { type ChannelTemplate, CUSTOM } from './types'

type TemplatePickerProps = {
  templates: ChannelTemplate[]
  onSelect: (id: string) => void
}

/** 网格里一张卡片所需的字段;模板的名称与说明取自词典,自定义模式不是后端模板。 */
type PickerItem = { id: string; name: string; description: string }

/** 渠道创建的选类型步:按分类分组的品牌徽标网格,自定义模式收在末尾。 */
export function TemplatePicker({ templates, onSelect }: TemplatePickerProps) {
  const m = useMessages()
  const toItem = (tpl: ChannelTemplate): PickerItem => ({
    id: tpl.id,
    ...m.channelTemplates.templates[tpl.id],
  })
  const custom: PickerItem = {
    id: CUSTOM,
    name: m.channels.customName,
    description: m.channels.customDesc,
  }
  const groups = [
    { key: 'object_storage', label: m.channels.groupObjectStorage },
    { key: 'drive', label: m.channels.groupDrive },
    { key: CUSTOM, label: m.channels.groupAdvanced },
  ].map((group) => ({
    ...group,
    items:
      group.key === CUSTOM
        ? [custom]
        : templates.filter((tpl) => tpl.category === group.key).map(toItem),
  }))

  return (
    <div className='space-y-6'>
      {groups
        .filter((group) => group.items.length > 0)
        .map((group) => (
          <section key={group.key} className='space-y-2.5'>
            <h3 className='text-xs font-medium tracking-wider text-muted-foreground uppercase'>
              {group.label}
            </h3>
            <div className='grid grid-cols-1 gap-2.5 sm:grid-cols-2'>
              {group.items.map((tpl) => (
                <button
                  key={tpl.id}
                  type='button'
                  onClick={() => onSelect(tpl.id)}
                  className='flex items-center gap-3 rounded-lg border bg-card p-3 text-start transition-colors hover:border-primary/60 hover:bg-accent/40 focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none'
                >
                  <TypeBadge id={tpl.id} />
                  <span className='min-w-0'>
                    <span className='block truncate text-sm font-medium'>
                      {tpl.name}
                    </span>
                    <span className='block truncate text-xs text-muted-foreground'>
                      {tpl.description}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          </section>
        ))}
    </div>
  )
}
