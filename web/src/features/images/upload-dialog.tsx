import { useCallback, useEffect, useRef, useState } from 'react'
import { t } from '@/i18n'
import {
  CircleCheck,
  CircleX,
  Copy,
  ImagePlus,
  Link2,
  Loader2,
} from 'lucide-react'
import { toast } from 'sonner'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { type UploadOptions } from './data'
import {
  defaultOptionsDraft,
  draftToOptions,
  type OptionsDraft,
} from './upload-options'
import { UploadOptionsFields } from './upload-options-fields'
import { type UploadEntry, useUploadQueue } from './upload-queue'

/** 与后端格式白名单一致的选择器提示;真校验在服务端。 */
const ACCEPT = 'image/jpeg,image/png,image/gif,image/webp'

type UploadDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onUploaded: () => void // 有新图落库时回调,用于刷新列表
}

/** 上传对话框:拖拽 / 点选 / 粘贴多图受控并发上传,或由服务端转存远端地址;关闭不打断进行中的上传。 */
export function UploadDialog({
  open,
  onOpenChange,
  onUploaded,
}: UploadDialogProps) {
  const m = useMessages()
  const config = useServerConfig()
  const queue = useUploadQueue(onUploaded)
  const [draft, setDraft] = useState<OptionsDraft>(defaultOptionsDraft)
  const [sourceURL, setSourceURL] = useState('')
  const [fetching, setFetching] = useState(false)

  /** 当前选项;自定义档未填小时数时拒绝。 */
  const options = useCallback(
    (): UploadOptions | null =>
      draftToOptions(draft, m.images.ttlCustomRequired),
    [draft, m]
  )

  const addFiles = useCallback(
    (files: Iterable<File>) => {
      const opts = options()
      if (!opts) return
      for (const file of files) {
        queue.enqueueFile(file, file.name || m.images.clipboardImage, opts)
      }
    },
    [options, queue, m]
  )

  const fetchFromURL = async () => {
    const target = sourceURL.trim()
    const opts = options()
    if (!target || !opts) return
    setFetching(true)
    const ok = await queue.fetchURL(target, opts)
    setFetching(false)
    if (ok) setSourceURL('')
  }

  // 对话框打开期间监听全局 paste:剪贴板里的图片直接入队。
  useEffect(() => {
    if (!open) return
    const onPaste = (e: ClipboardEvent) => {
      const files = Array.from(e.clipboardData?.items ?? [])
        .filter((it) => it.kind === 'file' && it.type.startsWith('image/'))
        .map((it) => it.getAsFile())
        .filter((f): f is File => f !== null)
      if (files.length > 0) {
        e.preventDefault()
        addFiles(files)
      }
    }
    document.addEventListener('paste', onPaste)
    return () => document.removeEventListener('paste', onPaste)
  }, [open, addFiles])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>{m.images.uploadTitle}</DialogTitle>
          <DialogDescription>{m.images.uploadDesc}</DialogDescription>
        </DialogHeader>
        <Tabs defaultValue='files'>
          <TabsList className='grid w-full grid-cols-2'>
            <TabsTrigger value='files'>
              <ImagePlus />
              {m.images.tabFiles}
            </TabsTrigger>
            <TabsTrigger value='url'>
              <Link2 />
              {m.images.tabUrl}
            </TabsTrigger>
          </TabsList>
          <TabsContent value='files' className='pt-3'>
            <Dropzone
              onFiles={addFiles}
              limits={t(m.images.dropLimits, {
                max: config?.max_upload_mb ?? '',
              })}
            />
          </TabsContent>
          <TabsContent value='url' className='space-y-2 pt-3'>
            <div className='flex gap-2'>
              <Input
                type='url'
                placeholder={m.images.urlPlaceholder}
                value={sourceURL}
                disabled={fetching}
                onChange={(e) => setSourceURL(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void fetchFromURL()
                }}
              />
              <Button
                disabled={fetching || sourceURL.trim() === ''}
                onClick={() => void fetchFromURL()}
              >
                {fetching && <Loader2 className='animate-spin' />}
                {m.images.fetch}
              </Button>
            </div>
            <p className='text-xs text-muted-foreground'>{m.images.urlHint}</p>
          </TabsContent>
        </Tabs>

        <UploadOptionsFields
          showPrivate
          draft={draft}
          onChange={setDraft}
          footnote={m.images.appliesLater}
        />

        {queue.entries.length > 0 && (
          <EntryList entries={queue.entries} onClear={queue.clearSettled} />
        )}
      </DialogContent>
    </Dialog>
  )
}

/** 拖放区:点击选文件、拖入或粘贴。 */
function Dropzone({
  onFiles,
  limits,
}: {
  onFiles: (files: FileList) => void
  limits: string
}) {
  const m = useMessages()
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  return (
    <label
      data-dragging={dragging}
      className={cn(
        'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed px-6 py-8 text-center transition-colors hover:border-primary/60 hover:bg-accent/30',
        dragging && 'border-primary bg-accent/40'
      )}
      onDragOver={(e) => {
        e.preventDefault()
        setDragging(true)
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDragging(false)
        if (e.dataTransfer.files.length > 0) onFiles(e.dataTransfer.files)
      }}
    >
      <input
        ref={inputRef}
        type='file'
        multiple
        accept={ACCEPT}
        className='sr-only'
        onChange={(e) => {
          if (e.target.files && e.target.files.length > 0)
            onFiles(e.target.files)
          e.target.value = ''
        }}
      />
      <span className='flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary'>
        <ImagePlus className='size-6' />
      </span>
      <span className='text-sm font-medium'>{m.images.dropTitle}</span>
      <span className='text-xs text-muted-foreground'>
        <span className='block'>{m.images.dropHint}</span>
        <span className='block'>{limits}</span>
      </span>
    </label>
  )
}

function EntryList({
  entries,
  onClear,
}: {
  entries: UploadEntry[]
  onClear: () => void
}) {
  const m = useMessages()
  const settled = entries.some((e) => e.status !== 'uploading')
  const copy = async (url: string) => {
    if (await copyText(url)) toast.success(m.images.linkCopied)
  }
  return (
    <div className='space-y-2'>
      <ul className='max-h-60 space-y-2 overflow-y-auto pe-1'>
        {entries.map((e) => (
          <li
            key={e.uid}
            className='flex items-center gap-3 rounded-lg border px-3 py-2'
          >
            <StatusIcon status={e.status} />
            <div className='min-w-0 flex-1'>
              <p className='truncate text-sm'>{e.name}</p>
              {e.url && (
                <p className='truncate font-mono text-xs text-muted-foreground'>
                  {e.url}
                </p>
              )}
              {e.error && <p className='text-xs text-destructive'>{e.error}</p>}
            </div>
            {e.url && (
              <Button
                size='sm'
                variant='ghost'
                className='h-7'
                onClick={() => copy(e.url as string)}
              >
                <Copy />
                {m.common.copy}
              </Button>
            )}
          </li>
        ))}
      </ul>
      {settled && (
        <div className='flex justify-end'>
          <Button variant='ghost' size='sm' onClick={onClear}>
            {m.images.clearList}
          </Button>
        </div>
      )}
    </div>
  )
}

function StatusIcon({ status }: { status: UploadEntry['status'] }) {
  const m = useMessages()
  if (status === 'uploading') {
    return (
      <Loader2
        className='size-5 shrink-0 animate-spin text-primary'
        aria-label={m.images.uploading}
      />
    )
  }
  if (status === 'done') {
    return (
      <CircleCheck
        className='size-5 shrink-0 text-success'
        aria-label={m.images.uploadDone}
      />
    )
  }
  return (
    <CircleX
      className='size-5 shrink-0 text-destructive'
      aria-label={m.images.uploadFailed}
    />
  )
}
