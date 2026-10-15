import { useState } from 'react'
import { type UseQueryResult, useQuery } from '@tanstack/react-query'
import { useMessages } from '@/context/locale-provider'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { QueryState } from '@/components/query-state'
import { ChannelForm } from './channel-form'
import { channelConfigQueryOptions, templatesQueryOptions } from './data'
import { TemplatePicker } from './template-picker'
import {
  type ChannelItem,
  type ChannelTemplate,
  CUSTOM,
  type EditConfig,
} from './types'

type ChannelSheetProps = {
  open: boolean
  channel: ChannelItem | null // 编辑目标;null 为创建
  onOpenChange: (open: boolean) => void
}

/** 渠道创建 / 编辑侧栏:创建先选类型再配置,编辑直接进入配置步。 */
export function ChannelSheet({
  open,
  channel,
  onOpenChange,
}: ChannelSheetProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className='flex w-full flex-col gap-0 p-0 sm:max-w-xl'
        // 表单内容多,点到遮罩不关闭,避免误触丢失已填内容;Esc 与关闭按钮照常。
        onInteractOutside={(event) => event.preventDefault()}
      >
        {open && (
          <SheetBody
            key={channel?.id ?? 'new'}
            channel={channel}
            onClose={() => onOpenChange(false)}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

type SheetBodyProps = {
  channel: ChannelItem | null
  onClose: () => void
}

/** 侧栏内容:头部随步骤变化,模板清单与编辑回显就绪后进入选类型步或配置步。 */
function SheetBody({ channel, onClose }: SheetBodyProps) {
  const m = useMessages()
  const templates = useQuery(templatesQueryOptions())
  const config = useQuery({
    ...channelConfigQueryOptions(channel?.id ?? 0),
    enabled: channel !== null,
  })
  const [pickedId, setPickedId] = useState<string | null>(null)

  // 编辑模式的类型随回读的配置确定;模板已不存在时按自定义配置展示。
  const prefill = channel ? config.data : undefined
  const typeId = channel ? prefill?.template || CUSTOM : pickedId
  const template =
    typeId === null || typeId === CUSTOM
      ? null
      : (templates.data?.find((tpl) => tpl.id === typeId) ?? null)

  return (
    <>
      <SheetHeader className='border-b'>
        <SheetTitle>{channel ? m.channels.edit : m.channels.create}</SheetTitle>
        <SheetDescription>
          {channel
            ? channel.name
            : typeId === null
              ? m.channels.pickType
              : template
                ? m.channelTemplates.templates[template.id].name
                : m.channels.customName}
        </SheetDescription>
      </SheetHeader>
      <SheetStep
        templates={templates}
        config={channel ? config : null}
        channel={channel}
        typeId={typeId}
        template={template}
        onPick={setPickedId}
        onClose={onClose}
      />
    </>
  )
}

type SheetStepProps = {
  templates: UseQueryResult<ChannelTemplate[]>
  config: UseQueryResult<EditConfig> | null // 编辑回显;创建为 null
  channel: ChannelItem | null
  typeId: string | null // null 表示仍在选类型
  template: ChannelTemplate | null
  onPick: (id: string | null) => void
  onClose: () => void
}

function SheetStep({
  templates,
  config,
  channel,
  typeId,
  template,
  onPick,
  onClose,
}: SheetStepProps) {
  const failing = templates.isError
    ? templates
    : config?.isError
      ? config
      : null
  if (failing) {
    return (
      <div className='p-4'>
        <QueryState query={failing}>{null}</QueryState>
      </div>
    )
  }
  if (!templates.data || (config && !config.data)) {
    return <SheetSkeleton />
  }
  if (typeId === null) {
    return (
      <div className='min-h-0 flex-1 overflow-y-auto p-4'>
        <TemplatePicker templates={templates.data} onSelect={onPick} />
      </div>
    )
  }
  return (
    <ChannelForm
      key={typeId}
      template={template}
      channel={channel}
      prefill={config?.data}
      onBack={channel ? undefined : () => onPick(null)}
      onSaved={onClose}
      onCancel={onClose}
    />
  )
}

function SheetSkeleton() {
  return (
    <div className='space-y-4 p-4'>
      <Skeleton className='h-9 w-2/3' />
      <Skeleton className='h-9 w-full' />
      <Skeleton className='h-9 w-full' />
      <Skeleton className='h-24 w-full' />
    </div>
  )
}
