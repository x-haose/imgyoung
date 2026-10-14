import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { errorMessage } from '@/lib/errors'
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
import { SecretReveal } from './secret-reveal'

type Props = {
  open: boolean
  onClose: () => void
  title: string
  desc?: string
  confirmText?: string
  destructive?: boolean
  children?: React.ReactNode // 确认前的表单内容
  /** 执行动作;返回非空字符串即当作只显示一次的密钥展示出来 */
  run: () => Promise<string | void>
}

/** 动作的通用确认框:动作返回了密钥就只显示这一次。 */
export function ActionDialog({
  open,
  onClose,
  title,
  desc,
  confirmText,
  destructive,
  children,
  run,
}: Props) {
  const [secret, setSecret] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const m = useMessages()

  const close = () => {
    setSecret('')
    setError('')
    onClose()
  }
  const go = async () => {
    setBusy(true)
    setError('')
    try {
      const result = await run()
      if (result) setSecret(result)
      else close()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {desc && <DialogDescription>{desc}</DialogDescription>}
        </DialogHeader>
        {secret ? <SecretReveal secret={secret} /> : children}
        {error && <p className='text-sm text-destructive'>{error}</p>}
        <DialogFooter>
          {secret ? (
            <Button onClick={close}>{m.common.close}</Button>
          ) : (
            <>
              <Button variant='outline' onClick={close}>
                {m.common.cancel}
              </Button>
              <Button
                variant={destructive ? 'destructive' : 'default'}
                disabled={busy}
                onClick={go}
              >
                {busy && <Loader2 className='animate-spin' />}
                {confirmText ?? m.common.confirm}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
