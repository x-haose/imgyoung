import { useId, useState } from 'react'
import { KeyRound, TriangleAlert } from 'lucide-react'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { SecretReveal } from '@/components/secret-reveal'
import { type IssuedToken } from './data'

export type IssuedResult = {
  token: IssuedToken
  rotated: boolean // 来自轮转而非新签发,标题措辞不同
}

type IssuedTokenDialogProps = {
  issued: IssuedResult | null
  onClose: () => void
}

/**
 * 一次性明文展示:遮罩点击、Esc 与右上角叉都不关闭,唯一出口是勾选「已保存」后的关闭按钮,
 * 防止明文还没抄下就被误关。
 */
export function IssuedTokenDialog({ issued, onClose }: IssuedTokenDialogProps) {
  const m = useMessages()
  const [saved, setSaved] = useState(false)
  const checkboxId = useId()

  const close = () => {
    setSaved(false)
    onClose()
  }

  return (
    <Dialog open={issued !== null}>
      <DialogContent
        className='sm:max-w-lg'
        showCloseButton={false}
        onEscapeKeyDown={(e) => e.preventDefault()}
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <KeyRound className='size-5 text-primary' />
            {issued?.rotated
              ? m.tokens.issued.rotatedTitle
              : m.tokens.issued.title}
          </DialogTitle>
          <DialogDescription className='font-mono'>
            {issued?.token.name}
          </DialogDescription>
        </DialogHeader>
        <Alert className='border-warning/40 bg-warning/10'>
          <TriangleAlert className='text-warning' />
          <AlertTitle className='line-clamp-none'>
            {m.tokens.issued.warning}
          </AlertTitle>
        </Alert>
        {issued && <SecretReveal secret={issued.token.token} />}
        <p className='text-xs text-muted-foreground'>{m.tokens.issued.usage}</p>
        <DialogFooter className='items-center sm:justify-between'>
          <div className='flex items-center gap-2'>
            <Checkbox
              id={checkboxId}
              checked={saved}
              onCheckedChange={(v) => setSaved(v === true)}
            />
            <Label htmlFor={checkboxId} className='font-normal'>
              {m.tokens.issued.confirmSaved}
            </Label>
          </div>
          <Button disabled={!saved} onClick={close}>
            {m.tokens.issued.close}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
