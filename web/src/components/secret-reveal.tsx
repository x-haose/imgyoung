import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { copyText } from '@/lib/clipboard'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'

/** 只显示一次的密钥:明文加复制。 */
export function SecretReveal({ secret }: { secret: string }) {
  const [copied, setCopied] = useState(false)
  const m = useMessages()
  const copy = async () => {
    if (!(await copyText(secret))) return
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className='flex items-center gap-2 rounded-md border bg-muted/40 p-2'>
      <code className='flex-1 font-mono text-sm break-all select-all'>
        {secret}
      </code>
      <Button variant='outline' size='sm' onClick={copy}>
        {copied ? <Check /> : <Copy />}
        {copied ? m.common.copied : m.common.copy}
      </Button>
    </div>
  )
}
