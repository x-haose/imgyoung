import { type Messages } from '@/i18n'
import {
  Cloud,
  MoreHorizontal,
  RefreshCw,
  ShieldOff,
  Trash2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { type TokenItem, tokenStatus } from './data'

/** 行上可触发的动作;s3 与 rotate 只对本人的有效 Token 开放,delete 只对失效记录开放。 */
export type TokenAction = 's3' | 'rotate' | 'revoke' | 'delete'

type RowActionsProps = {
  m: Messages
  token: TokenItem
  viewingSelf: boolean // 当前列表是否本人的 Token;看别人时不下发明文类动作
  onAction: (action: TokenAction, token: TokenItem) => void
}

export function RowActions({
  m,
  token,
  viewingSelf,
  onAction,
}: RowActionsProps) {
  const active = tokenStatus(token) === 'active'
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button variant='ghost' size='icon' className='size-8'>
          <MoreHorizontal />
          <span className='sr-only'>{m.tokens.actions.more}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end' className='min-w-44'>
        {active ? (
          <>
            {viewingSelf && (
              <>
                <DropdownMenuItem onClick={() => onAction('s3', token)}>
                  <Cloud />
                  {m.tokens.actions.s3}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => onAction('rotate', token)}>
                  <RefreshCw />
                  {m.tokens.actions.rotate}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
              </>
            )}
            <DropdownMenuItem
              variant='destructive'
              onClick={() => onAction('revoke', token)}
            >
              <ShieldOff />
              {m.tokens.actions.revoke}
            </DropdownMenuItem>
          </>
        ) : (
          <DropdownMenuItem
            variant='destructive'
            onClick={() => onAction('delete', token)}
          >
            <Trash2 />
            {m.tokens.actions.delete}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
