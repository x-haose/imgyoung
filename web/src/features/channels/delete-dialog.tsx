import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import { toast } from 'sonner'
import { useMessages } from '@/context/locale-provider'
import { Checkbox } from '@/components/ui/checkbox'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { deleteChannel, useInvalidateChannels } from './data'
import { type ChannelItem } from './types'

type DialogProps = {
  channel: ChannelItem | null // 目标渠道;null 为关闭
  onClose: () => void
}

/** 删除确认:可选同时清理远端对象;持有唯一副本被拒时后端原因经全局提示展示,对话框保持打开。 */
export function DeleteChannelDialog({ channel, onClose }: DialogProps) {
  const m = useMessages()
  const invalidate = useInvalidateChannels()
  const [purge, setPurge] = useState(true)
  const remove = useMutation({
    mutationFn: (vars: { id: number; purge: boolean }) =>
      deleteChannel(vars.id, vars.purge),
    onSuccess: (_, vars) => {
      toast.success(
        vars.purge ? m.channels.deletedPurging : m.channels.deletedKept
      )
      invalidate()
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open={channel !== null}
      onOpenChange={(open) => !open && onClose()}
      title={t(m.channels.deleteTitle, { name: channel?.name ?? '' })}
      desc={m.channels.deleteDesc}
      confirmText={purge ? m.channels.deleteAndPurge : m.channels.removeOnly}
      destructive
      isLoading={remove.isPending}
      handleConfirm={() => channel && remove.mutate({ id: channel.id, purge })}
    >
      <label className='flex items-start gap-3 rounded-lg border p-3'>
        <Checkbox
          checked={purge}
          onCheckedChange={(checked) => setPurge(checked === true)}
          className='mt-0.5'
        />
        <span className='space-y-1'>
          <span className='block text-sm font-medium'>
            {m.channels.purgeLabel}
          </span>
          <span className='block text-xs leading-relaxed text-muted-foreground'>
            {purge ? m.channels.purgeOnHint : m.channels.purgeOffHint}
          </span>
        </span>
      </label>
    </ConfirmDialog>
  )
}

/** 清理中的渠道放弃清理、立即移除;远端尚未删除的对象原样保留。 */
export function RemoveNowDialog({ channel, onClose }: DialogProps) {
  const m = useMessages()
  const invalidate = useInvalidateChannels()
  const remove = useMutation({
    mutationFn: (id: number) => deleteChannel(id, false),
    onSuccess: () => {
      toast.success(m.channels.deletedKept)
      invalidate()
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open={channel !== null}
      onOpenChange={(open) => !open && onClose()}
      title={t(m.channels.removeNowTitle, { name: channel?.name ?? '' })}
      desc={m.channels.removeNowDesc}
      confirmText={m.channels.actionRemoveNow}
      destructive
      isLoading={remove.isPending}
      handleConfirm={() => channel && remove.mutate(channel.id)}
    />
  )
}
