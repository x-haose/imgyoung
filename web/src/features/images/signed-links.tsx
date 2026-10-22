import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Copy } from 'lucide-react'
import { toast } from 'sonner'
import { copyText } from '@/lib/clipboard'
import { formatTime } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { copyFormats, shareURL, signImageURL } from './data'

const SECONDS_PER_HOUR = 3600

/** 可选的签名链接有效期,单位小时;取值是产品给定的几档,不是后端限制。 */
const TTL_HOURS = [1, 24, 168, 720] as const

/** 私有图的签名链接:选有效期、生成、复制;私有图没有永久链接。 */
export function SignedLinks({
  id,
  baseURL,
}: {
  id: string
  baseURL: string | undefined
}) {
  const m = useMessages().images
  const [hours, setHours] = useState<number>(24)
  const sign = useMutation({
    mutationFn: () => signImageURL(id, hours * SECONDS_PER_HOUR),
  })
  const url = sign.data ? shareURL(sign.data.url, baseURL) : ''
  const copy = async (text: string, done: string) => {
    if (await copyText(text)) toast.success(done)
  }
  return (
    <section className='space-y-2'>
      <h3 className='text-xs font-medium tracking-wide text-muted-foreground uppercase'>
        {m.signedTitle}
      </h3>
      <p className='text-xs text-muted-foreground'>{m.signedHint}</p>
      <div className='flex items-center gap-2'>
        <Select
          value={String(hours)}
          onValueChange={(v) => setHours(Number(v))}
        >
          <SelectTrigger className='w-36' aria-label={m.signedValidity}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TTL_HOURS.map((h) => (
              <SelectItem key={h} value={String(h)}>
                {m.signedTtl[String(h) as keyof typeof m.signedTtl]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          variant='outline'
          disabled={sign.isPending}
          onClick={() => sign.mutate()}
        >
          {m.signedGenerate}
        </Button>
      </div>
      {sign.data && (
        <>
          <div className='flex items-center gap-2'>
            <Input
              readOnly
              value={url}
              className='font-mono text-xs'
              onFocus={(e) => e.currentTarget.select()}
            />
            <Button
              variant='outline'
              size='icon'
              onClick={() => copy(url, m.linkCopied)}
            >
              <Copy />
              <span className='sr-only'>{m.copyLink}</span>
            </Button>
          </div>
          <p className='text-xs text-muted-foreground'>
            {t(m.signedExpiresAt, { time: formatTime(sign.data.expires_at) })}
          </p>
          <div className='flex flex-wrap items-center gap-1.5'>
            <span className='text-xs text-muted-foreground'>{m.copyAs}</span>
            {copyFormats.map((f) => (
              <Button
                key={f.key}
                variant='outline'
                size='sm'
                className='h-7 px-2 text-xs'
                onClick={() =>
                  copy(f.build(url), t(m.copiedAs, { format: f.label }))
                }
              >
                {f.label}
              </Button>
            ))}
          </div>
        </>
      )}
    </section>
  )
}
