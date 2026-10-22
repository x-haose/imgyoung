import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { t } from '@/i18n'
import { toast } from 'sonner'
import { errorText } from '@/lib/server-text'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { imagesQueryKey } from './data'
import { batchTagImages, parseTags, tagsQueryKey } from './tags'

type BatchTagsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  ids: string[] // 选中的图片
  onDone: () => void // 修改完成后收起多选
}

/** 给选中的图片批量追加或移除标签。 */
export function BatchTagsDialog({
  open,
  onOpenChange,
  ids,
  onDone,
}: BatchTagsDialogProps) {
  const messages = useMessages()
  const m = messages.images
  const queryClient = useQueryClient()
  const [add, setAdd] = useState('')
  const [remove, setRemove] = useState('')
  const addTags = parseTags(add)
  const removeTags = parseTags(remove)

  const run = useMutation({
    mutationFn: () => batchTagImages(ids, addTags, removeTags),
    onSuccess: (res) => {
      if (res.failed.length > 0) {
        toast.warning(
          t(m.batchTagsPartial, {
            updated: res.updated,
            failed: res.failed.length,
            error: errorText(res.failed[0]),
          })
        )
      } else {
        toast.success(t(m.batchTagsDone, { count: res.updated }))
      }
      void queryClient.invalidateQueries({ queryKey: imagesQueryKey })
      void queryClient.invalidateQueries({ queryKey: tagsQueryKey })
      setAdd('')
      setRemove('')
      onOpenChange(false)
      onDone()
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {t(m.batchTagsTitle, { count: ids.length })}
          </DialogTitle>
          <DialogDescription>{m.batchTagsDesc}</DialogDescription>
        </DialogHeader>
        <div className='space-y-4'>
          <div className='space-y-2'>
            <Label htmlFor='batch-tags-add'>{m.batchTagsAdd}</Label>
            <Input
              id='batch-tags-add'
              value={add}
              placeholder={m.tagsPlaceholder}
              onChange={(e) => setAdd(e.target.value)}
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='batch-tags-remove'>{m.batchTagsRemove}</Label>
            <Input
              id='batch-tags-remove'
              value={remove}
              placeholder={m.tagsPlaceholder}
              onChange={(e) => setRemove(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {messages.common.cancel}
          </Button>
          <Button
            disabled={
              run.isPending || (addTags.length === 0 && removeTags.length === 0)
            }
            onClick={() => run.mutate()}
          >
            {m.batchTagsApply}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
