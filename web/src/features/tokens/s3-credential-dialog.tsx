import { useQuery } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Copy, Info } from 'lucide-react'
import { toast } from 'sonner'
import { copyText } from '@/lib/clipboard'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { QueryState } from '@/components/query-state'
import {
  type S3Credential,
  s3CredentialQueryOptions,
  type TokenItem,
} from './data'

type S3CredentialDialogProps = {
  token: TokenItem | null
  onClose: () => void
}

/** 配置项的呈现顺序,与 PicGo「Amazon S3」表单字段一一对应,照抄即可。 */
const fieldOrder: (keyof S3Credential)[] = [
  'access_key_id',
  'secret_access_key',
  'bucket',
  'region',
  'endpoint',
  'url_prefix',
]

/**
 * 展示一枚 Token 的 S3 协议凭证供客户端照填。
 * 与明文弹窗刻意不同:密钥由主密钥按需派生、可随时重看,故允许随手关闭。
 */
export function S3CredentialDialog({
  token,
  onClose,
}: S3CredentialDialogProps) {
  return (
    <Dialog open={token !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-xl'>
        {token && <S3CredentialBody token={token} />}
      </DialogContent>
    </Dialog>
  )
}

function S3CredentialBody({ token }: { token: TokenItem }) {
  const m = useMessages()
  const query = useQuery(s3CredentialQueryOptions(token.id))
  const hints = m.tokens.s3.hints as Partial<Record<keyof S3Credential, string>>

  const copy = async (value: string) => {
    if (await copyText(value)) toast.success(m.common.copied)
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(m.tokens.s3.title, { name: token.name })}</DialogTitle>
        <DialogDescription>{m.tokens.s3.intro}</DialogDescription>
      </DialogHeader>
      <QueryState query={query}>
        <dl className='divide-y rounded-lg border'>
          {fieldOrder.map((key) => (
            <div
              key={key}
              className='grid gap-1 px-4 py-3 sm:grid-cols-[9rem_1fr]'
            >
              <dt className='text-sm font-medium'>{m.tokens.s3.fields[key]}</dt>
              <dd className='min-w-0'>
                <div className='flex items-start gap-2'>
                  <code className='min-w-0 flex-1 font-mono text-sm break-all select-all'>
                    {query.data?.[key]}
                  </code>
                  <Button
                    variant='ghost'
                    size='icon'
                    className='size-7 shrink-0 text-muted-foreground'
                    onClick={() => query.data && copy(query.data[key])}
                  >
                    <Copy />
                    <span className='sr-only'>{m.tokens.s3.copy}</span>
                  </Button>
                </div>
                {hints[key] && (
                  <p className='mt-1 text-xs text-muted-foreground'>
                    {hints[key]}
                  </p>
                )}
              </dd>
            </div>
          ))}
        </dl>
      </QueryState>
      <Alert>
        <Info />
        <AlertTitle className='line-clamp-none'>{m.tokens.s3.note}</AlertTitle>
        <AlertDescription>{m.tokens.s3.pathStyle}</AlertDescription>
      </Alert>
    </>
  )
}
