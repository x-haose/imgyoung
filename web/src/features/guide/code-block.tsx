import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'

/** 终端风格代码块:两种主题下都用深色底,右上角一键复制。 */
export function CodeBlock({
  code,
  className,
}: {
  code: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  const m = useMessages()
  const copy = async () => {
    if (!(await copyText(code))) return
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div
      className={cn(
        'relative rounded-lg border border-zinc-800 bg-zinc-950 text-zinc-100',
        className
      )}
    >
      <Button
        type='button'
        variant='ghost'
        size='icon'
        onClick={copy}
        className='absolute end-1.5 top-1.5 size-7 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100'
      >
        {copied ? <Check className='text-success' /> : <Copy />}
        <span className='sr-only'>
          {copied ? m.common.copied : m.common.copy}
        </span>
      </Button>
      <pre className='overflow-x-auto px-4 py-3 pe-12 font-mono text-[12.5px] leading-relaxed'>
        {code}
      </pre>
    </div>
  )
}

/** 行内可复制的短值:地址、字段名、请求头。 */
export function CopyValue({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  const m = useMessages()
  const copy = async () => {
    if (!(await copyText(value))) return
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <span className='inline-flex max-w-full items-center gap-1'>
      <code className='truncate rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs'>
        {value}
      </code>
      <Button
        type='button'
        variant='ghost'
        size='icon'
        onClick={copy}
        className='size-6 shrink-0 text-muted-foreground'
      >
        {copied ? (
          <Check className='size-3.5 text-success' />
        ) : (
          <Copy className='size-3.5' />
        )}
        <span className='sr-only'>
          {copied ? m.common.copied : m.common.copy}
        </span>
      </Button>
    </span>
  )
}
