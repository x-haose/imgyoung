import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { t } from '@/i18n'
import {
  ChevronDown,
  FileText,
  Globe,
  Link2,
  Loader2,
  Paperclip,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/errors'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  draftToOptions,
  type OptionsDraft,
} from '@/features/images/upload-options'
import { UploadOptionsFields } from '@/features/images/upload-options-fields'
import {
  type CreateDraft,
  emptyDraft,
  hasInput,
  type SourceDocument,
  toRequest,
} from './create-request'
import { createImport, importsKey, type ImportMode } from './data'

/** 文本模式可选的文件扩展名。 */
const TEXT_ACCEPT = '.md,.markdown,.html,.htm,.txt'

type CreateImportDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** 新建导入对话框:三种输入各一个标签,下方是三种输入共用的高级选项;创建成功后进入任务详情。 */
export function CreateImportDialog({
  open,
  onOpenChange,
}: CreateImportDialogProps) {
  const m = useMessages().imports.dialog
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{m.title}</DialogTitle>
          <DialogDescription>{m.desc}</DialogDescription>
        </DialogHeader>
        <CreateForm onCancel={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

/** 表单状态随对话框内容一起挂载与卸载,关闭即清空。 */
function CreateForm({ onCancel }: { onCancel: () => void }) {
  const m = useMessages()
  const d = m.imports.dialog
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<CreateDraft>(emptyDraft)
  const patch = (changes: Partial<CreateDraft>) =>
    setDraft((prev) => ({ ...prev, ...changes }))

  const create = useMutation({
    mutationFn: createImport,
    onSuccess: (job) => {
      void queryClient.invalidateQueries({ queryKey: importsKey })
      void navigate({ to: '/imports/$id', params: { id: job.id } })
    },
  })

  const submit = () => {
    const options = draftToOptions(draft.options, m.images.ttlCustomRequired)
    if (options) create.mutate(toRequest(draft, options, d.pastedName))
  }
  const submitLabel = {
    images: d.submitImages,
    site: d.submitSite,
    text: d.submitText,
  }[draft.mode]

  return (
    <>
      <Tabs
        value={draft.mode}
        onValueChange={(mode) => patch({ mode: mode as ImportMode })}
      >
        <TabsList className='grid w-full grid-cols-3'>
          <TabsTrigger value='images'>
            <Link2 />
            {d.tabImages}
          </TabsTrigger>
          <TabsTrigger value='site'>
            <Globe />
            {d.tabSite}
          </TabsTrigger>
          <TabsTrigger value='text'>
            <FileText />
            {d.tabText}
          </TabsTrigger>
        </TabsList>
        <TabsContent value='images' className='pt-3'>
          <LinesField
            label={d.imagesLabel}
            placeholder={d.imagesPlaceholder}
            hint={d.imagesHint}
            value={draft.urls}
            onChange={(urls) => patch({ urls })}
          />
        </TabsContent>
        <TabsContent value='site' className='pt-3'>
          <LinesField
            label={d.siteLabel}
            placeholder={d.sitePlaceholder}
            hint={d.siteHint}
            value={draft.sites}
            onChange={(sites) => patch({ sites })}
          />
        </TabsContent>
        <TabsContent value='text' className='pt-3'>
          <TextSource draft={draft} onChange={patch} />
        </TabsContent>
      </Tabs>
      <AdvancedOptions
        options={draft.options}
        referer={draft.referer}
        onOptions={(options) => patch({ options })}
        onReferer={(referer) => patch({ referer })}
      />
      <DialogFooter>
        <Button variant='ghost' onClick={onCancel} disabled={create.isPending}>
          {m.common.cancel}
        </Button>
        <Button
          disabled={!hasInput(draft) || create.isPending}
          onClick={submit}
        >
          {create.isPending && <Loader2 className='animate-spin' />}
          {submitLabel}
        </Button>
      </DialogFooter>
    </>
  )
}

/** 多行输入:一行一个地址,等宽字体便于核对。 */
function LinesField({
  label,
  placeholder,
  hint,
  value,
  onChange,
}: {
  label: string
  placeholder: string
  hint: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <div className='space-y-2'>
      <Label>{label}</Label>
      <Textarea
        value={value}
        placeholder={placeholder}
        spellCheck={false}
        rows={6}
        className='max-h-64 font-mono text-xs'
        onChange={(e) => onChange(e.target.value)}
      />
      <p className='text-xs text-muted-foreground'>{hint}</p>
    </div>
  )
}

/** 文本与文件:粘贴文本、选择文件、基准地址。 */
function TextSource({
  draft,
  onChange,
}: {
  draft: CreateDraft
  onChange: (changes: Partial<CreateDraft>) => void
}) {
  const m = useMessages().imports.dialog
  const inputRef = useRef<HTMLInputElement>(null)

  /** 浏览器把文件读成文本后随请求提交;某个文件读不出来就整批不加入并提示。 */
  const addFiles = async (picked: FileList) => {
    const read: SourceDocument[] = []
    for (const file of picked) {
      try {
        read.push({ name: file.name, text: await file.text() })
      } catch (err) {
        toast.error(
          t(m.readFailed, { name: file.name, error: errorMessage(err) })
        )
        return
      }
    }
    onChange({ files: [...draft.files, ...read] })
  }

  return (
    <div className='space-y-3'>
      <div className='space-y-2'>
        <Label>{m.textLabel}</Label>
        <Textarea
          value={draft.text}
          placeholder={m.textPlaceholder}
          spellCheck={false}
          rows={5}
          className='max-h-56 font-mono text-xs'
          onChange={(e) => onChange({ text: e.target.value })}
        />
      </div>
      <div className='space-y-2'>
        <div className='flex flex-wrap items-center gap-x-3 gap-y-1'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => inputRef.current?.click()}
          >
            <Paperclip />
            {m.pickFiles}
          </Button>
          <span className='text-xs text-muted-foreground'>{m.filesHint}</span>
        </div>
        <input
          ref={inputRef}
          type='file'
          multiple
          accept={TEXT_ACCEPT}
          className='sr-only'
          onChange={(e) => {
            if (e.target.files && e.target.files.length > 0) {
              void addFiles(e.target.files)
            }
            e.target.value = ''
          }}
        />
        {draft.files.length > 0 && (
          <ul className='flex flex-wrap gap-2'>
            {draft.files.map((f, i) => (
              <li
                key={`${i}-${f.name}`}
                className='flex max-w-full items-center gap-1.5 rounded-md border bg-muted/40 py-1 ps-2.5 pe-1 text-xs'
              >
                <FileText className='size-3.5 shrink-0 text-muted-foreground' />
                <span className='truncate'>{f.name}</span>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  className='size-5'
                  aria-label={t(m.removeFile, { name: f.name })}
                  onClick={() =>
                    onChange({ files: draft.files.filter((_, j) => j !== i) })
                  }
                >
                  <X className='size-3' />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className='space-y-2'>
        <Label htmlFor='import-base-url'>{m.baseUrl}</Label>
        <Input
          id='import-base-url'
          type='url'
          value={draft.baseURL}
          placeholder={m.baseUrlPlaceholder}
          onChange={(e) => onChange({ baseURL: e.target.value })}
        />
        <p className='text-xs text-muted-foreground'>{m.baseUrlHint}</p>
      </div>
    </div>
  )
}

/** 高级选项:有效期、EXIF 档位与 Referer,默认折叠。 */
function AdvancedOptions({
  options,
  referer,
  onOptions,
  onReferer,
}: {
  options: OptionsDraft
  referer: string
  onOptions: (options: OptionsDraft) => void
  onReferer: (referer: string) => void
}) {
  const m = useMessages().imports.dialog
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className='flex items-center gap-1.5 rounded-md text-sm font-medium text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50'>
        <ChevronDown
          className={cn('size-4 transition-transform', !open && '-rotate-90')}
        />
        {m.advanced}
      </CollapsibleTrigger>
      <CollapsibleContent className='space-y-3 pt-3'>
        <UploadOptionsFields draft={options} onChange={onOptions} />
        <div className='space-y-2'>
          <Label htmlFor='import-referer'>{m.referer}</Label>
          <Input
            id='import-referer'
            type='url'
            value={referer}
            placeholder={m.refererPlaceholder}
            onChange={(e) => onReferer(e.target.value)}
          />
          <p className='text-xs text-muted-foreground'>{m.refererHint}</p>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
