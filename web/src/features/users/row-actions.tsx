import { type Messages } from '@/i18n'
import { KeyRound, MoreHorizontal, Pencil, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { type UserItem } from './data'

export type UserAction = 'edit' | 'reset' | 'delete'

type RowActionsProps = {
  m: Messages
  user: UserItem
  isSelf: boolean
  onAction: (action: UserAction, user: UserItem) => void
}

export function RowActions({ m, user, isSelf, onAction }: RowActionsProps) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button variant='ghost' size='icon' className='size-8'>
          <MoreHorizontal />
          <span className='sr-only'>{m.users.actions.more}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end' className='min-w-40'>
        <DropdownMenuItem onClick={() => onAction('edit', user)}>
          <Pencil />
          {m.users.actions.edit}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onAction('reset', user)}>
          <KeyRound />
          {m.users.actions.resetPassword}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant='destructive'
          disabled={isSelf}
          onClick={() => onAction('delete', user)}
        >
          <Trash2 />
          {m.users.actions.delete}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
