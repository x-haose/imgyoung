import { useRef } from 'react'
import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import {
  FileDown,
  Loader2,
  LockKeyhole,
  Server,
  TriangleAlert,
} from 'lucide-react'
import { isAbort, toastError } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { downloadRcloneConfig } from './data'
import { type ChannelItem } from './types'

type ExportConfigDialogProps = {
  channel: ChannelItem | null // 目标渠道;null 为关闭
  onClose: () => void
}

/**
 * 导出渠道的 rclone 配置:先说明文件里有什么、该怎么保管,确认后下载;下载成功才关闭,失败留在对话框里重试。
 * 关闭对话框即中止进行中的下载:对话框换给另一个渠道后,上一个渠道的下载不会再保存文件或关掉它。
 */
export function ExportConfigDialog({
  channel,
  onClose,
}: ExportConfigDialogProps) {
  const m = useMessages()
  const c = m.channels
  const encrypted = channel?.encrypted === true
  const pending = useRef<AbortController | null>(null)
  const dl = useMutation({
    mutationFn: (id: number) => {
      const ctl = new AbortController()
      pending.current = ctl
      return downloadRcloneConfig(id, ctl.signal)
    },
    onSuccess: onClose,
    onError: (err) => {
      if (!isAbort(err)) toastError(err)
    },
  })
  const close = () => {
    pending.current?.abort()
    onClose()
  }
  return (
    <Dialog open={channel !== null} onOpenChange={(open) => !open && close()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{c.exportConfigTitle}</DialogTitle>
          <DialogDescription>
            {t(c.exportConfigDesc, { name: channel?.name ?? '' })}
          </DialogDescription>
        </DialogHeader>
        <ul className='space-y-2.5 text-sm'>
          <ContentItem icon={<Server />} text={c.exportConfigBackend} />
          {encrypted && (
            <ContentItem icon={<LockKeyhole />} text={c.exportConfigCrypt} />
          )}
        </ul>
        <p className='text-xs leading-relaxed text-muted-foreground'>
          {c.exportConfigUsage}
        </p>
        <Alert className='border-warning/40 bg-warning/10'>
          <TriangleAlert className='text-warning' />
          <AlertTitle>
            {encrypted ? c.exportConfigWarnEncrypted : c.exportConfigWarnPlain}
          </AlertTitle>
          <AlertDescription>{c.exportConfigWarnDesc}</AlertDescription>
        </Alert>
        <DialogFooter>
          <Button variant='outline' onClick={close}>
            {m.common.cancel}
          </Button>
          {channel && (
            <Button
              disabled={dl.isPending}
              onClick={() => dl.mutate(channel.id)}
            >
              {dl.isPending ? (
                <Loader2 className='animate-spin' />
              ) : (
                <FileDown />
              )}
              {c.exportConfigDownload}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** 文件内容清单的一行:图标 + 说明。 */
function ContentItem({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <li className='flex items-start gap-3'>
      <span className='mt-0.5 text-muted-foreground [&_svg]:size-4'>
        {icon}
      </span>
      <span className='leading-relaxed'>{text}</span>
    </li>
  )
}
