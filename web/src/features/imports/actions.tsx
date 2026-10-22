import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Ban, Loader2, RotateCw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  canRetry,
  cancelImport,
  deleteImport,
  type ImportJob,
  importsKey,
  retryImport,
} from './data'

type Confirming = 'cancel' | 'delete' | null

/** 任务详情的操作:按状态给出取消、重试与删除,取消与删除先确认;非创建者(管理员看别人的任务)没有重试。 */
export function JobActions({
  job,
  isOwner,
}: {
  job: ImportJob
  isOwner: boolean // 重试只有创建者能做
}) {
  const m = useMessages().imports.detail
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState<Confirming>(null)
  const refresh = () => queryClient.invalidateQueries({ queryKey: importsKey })

  const cancel = useMutation({
    mutationFn: () => cancelImport(job.id),
    onSuccess: () => {
      toast.success(m.cancelled)
      setConfirming(null)
      return refresh()
    },
  })
  const retry = useMutation({
    mutationFn: () => retryImport(job.id),
    onSuccess: () => {
      toast.success(m.retried)
      return refresh()
    },
  })
  // 先离开详情页再失效缓存,已卸载的详情查询不会去取已删除的任务。
  const remove = useMutation({
    mutationFn: () => deleteImport(job.id),
    onSuccess: async () => {
      toast.success(m.deleted)
      await navigate({ to: '/imports' })
      await refresh()
    },
  })

  const cancellable = ['scanning', 'ready', 'importing'].includes(job.status)
  const deletable = !(job.status === 'scanning' || job.status === 'importing')
  return (
    <>
      {isOwner && canRetry(job) && (
        <Button disabled={retry.isPending} onClick={() => retry.mutate()}>
          {retry.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <RotateCw />
          )}
          {m.retry}
        </Button>
      )}
      {cancellable && (
        <Button variant='outline' onClick={() => setConfirming('cancel')}>
          <Ban />
          {m.cancel}
        </Button>
      )}
      {deletable && (
        <Button variant='outline' onClick={() => setConfirming('delete')}>
          <Trash2 />
          {m.delete}
        </Button>
      )}
      <ConfirmDialog
        open={confirming === 'cancel'}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={m.cancelTitle}
        desc={m.cancelDesc}
        confirmText={m.cancel}
        destructive
        isLoading={cancel.isPending}
        handleConfirm={() => cancel.mutate()}
      />
      <ConfirmDialog
        open={confirming === 'delete'}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={m.deleteTitle}
        desc={m.deleteDesc}
        confirmText={m.delete}
        destructive
        isLoading={remove.isPending}
        handleConfirm={() => remove.mutate()}
      />
    </>
  )
}
