import { useEffect, useRef, useState } from 'react'
import { t } from '@/i18n'
import { CheckCircle2, ExternalLink, Loader2 } from 'lucide-react'
import { errorMessage } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { fetchOAuthResult, startOAuth } from './data'
import {
  type ChannelTemplate,
  CLIENT_ID_KEY,
  OAUTH_CALLBACK_PATH,
  SECRET_PLACEHOLDER,
} from './types'

/** 授权结果轮询间隔。 */
const POLL_INTERVAL_MS = 1500

/** 轮询上限;须大于后端授权会话的存活时长,避免先于会话放弃。 */
const POLL_TIMEOUT_MS = 11 * 60 * 1000

type OAuthFieldProps = {
  template: ChannelTemplate
  inputs: Record<string, string> // 已填的其他字段:区域等影响授权端点的参数与自建应用凭证
  channelId?: number // 编辑目标;一键授权据此让后端还原字段中的哨兵
  value: string // 当前令牌;非空表示已授权,哨兵表示沿用已存令牌
  issuedToken: string // 服务端交还的令牌(测试连接时刷新所得);与 value 相同即视为已授权
  onChange: (token: string) => void
  disabled?: boolean
}

/**
 * 网盘授权控件:一键授权与粘贴令牌两条路径并列。
 * 一键授权要求实例已配置对外站点地址,并在模板的应用凭证字段填好自建应用;
 * 粘贴路径不依赖公网可达性,是无域名部署的唯一可行方式。
 */
export function OAuthField({
  template,
  inputs,
  channelId,
  value,
  issuedToken,
  onChange,
  disabled,
}: OAuthFieldProps) {
  const m = useMessages()
  const [error, setError] = useState('')
  const [waiting, setWaiting] = useState(false)
  // 一键授权换到的令牌;粘贴的文本在提交前只是草稿,不按「已授权」展示,经测试连接刷新后才算。
  const [flowToken, setFlowToken] = useState('')
  // 当前这轮等待的取消钩子;卸载或用户取消时让轮询静默退出。
  const cancelRef = useRef<() => void>(() => {})
  useEffect(() => () => cancelRef.current(), [])

  const cancel = () => {
    cancelRef.current()
    setWaiting(false)
  }

  const authorize = async () => {
    let cancelled = false
    cancelRef.current = () => {
      cancelled = true
    }
    setError('')
    setWaiting(true)
    try {
      const start = await startOAuth({
        template: template.id,
        inputs,
        ...(channelId ? { channel_id: channelId } : {}),
      })
      window.open(start.auth_url, '_blank', 'noopener,noreferrer')
      const deadline = Date.now() + POLL_TIMEOUT_MS
      while (Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS))
        if (cancelled) return
        const result = await fetchOAuthResult(start.state)
        if (result.error) {
          setError(result.error)
          return
        }
        if (result.token) {
          setFlowToken(result.token)
          onChange(result.token)
          return
        }
      }
      setError(m.channels.oauthTimeout)
    } catch (err) {
      if (!cancelled) setError(errorMessage(err))
    } finally {
      if (!cancelled) setWaiting(false)
    }
  }

  const hasClientId = (inputs[CLIENT_ID_KEY] ?? '').trim() !== ''
  const authorized =
    value !== '' &&
    (value === SECRET_PLACEHOLDER ||
      value === flowToken ||
      value === issuedToken)
  if (authorized) {
    return (
      <div className='flex flex-wrap items-center gap-3'>
        <Badge
          variant='outline'
          className='gap-1.5 border-success/40 bg-success/10 text-success'
        >
          <CheckCircle2 />
          {m.channels.oauthAuthorized}
          {value === SECRET_PLACEHOLDER && (
            <span className='font-normal opacity-80'>
              · {m.channels.oauthKept}
            </span>
          )}
        </Badge>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          disabled={disabled}
          onClick={() => onChange('')}
        >
          {m.channels.oauthReauthorize}
        </Button>
      </div>
    )
  }

  return (
    <Tabs defaultValue='oneclick' className='gap-3'>
      <TabsList>
        <TabsTrigger value='oneclick'>{m.channels.oauthOneClick}</TabsTrigger>
        <TabsTrigger value='paste'>{m.channels.oauthPaste}</TabsTrigger>
      </TabsList>
      <TabsContent value='oneclick' className='space-y-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            disabled={disabled || waiting || !hasClientId}
            onClick={authorize}
          >
            {waiting ? <Loader2 className='animate-spin' /> : <ExternalLink />}
            {waiting
              ? m.channels.oauthWaiting
              : t(m.channels.oauthGo, { name: template.name })}
          </Button>
          {waiting && (
            <Button type='button' variant='ghost' onClick={cancel}>
              {m.channels.oauthCancel}
            </Button>
          )}
        </div>
        <p className='text-xs leading-relaxed text-muted-foreground'>
          {t(m.channels.oauthHint, { path: OAUTH_CALLBACK_PATH })}
        </p>
      </TabsContent>
      <TabsContent value='paste' className='space-y-3'>
        <Textarea
          rows={3}
          placeholder={m.channels.oauthPastePlaceholder}
          disabled={disabled}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
        <p className='text-xs leading-relaxed text-muted-foreground'>
          {t(m.channels.oauthPasteHint, {
            command: `rclone authorize "${template.driver}"`,
          })}
        </p>
      </TabsContent>
      {error && (
        <Alert variant='destructive'>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
    </Tabs>
  )
}
