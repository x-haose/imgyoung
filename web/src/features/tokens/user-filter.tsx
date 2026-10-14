import { useQuery } from '@tanstack/react-query'
import { useMessages } from '@/context/locale-provider'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { usersQueryOptions } from '@/features/users/data'

type UserFilterProps = {
  selfId: string
  value: string // 当前查看的用户 id
  onChange: (userId: string) => void
}

/** 管理员按用户查看 Token;本人项带标记,放在最前。 */
export function UserFilter({ selfId, value, onChange }: UserFilterProps) {
  const m = useMessages()
  const users = useQuery(usersQueryOptions())
  const items = [...(users.data?.items ?? [])].sort((a, b) =>
    a.id === selfId ? -1 : b.id === selfId ? 1 : a.name.localeCompare(b.name)
  )
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className='w-44'>
        <SelectValue placeholder={m.tokens.viewingUser} />
      </SelectTrigger>
      <SelectContent>
        {items.map((u) => (
          <SelectItem key={u.id} value={u.id}>
            {u.name}
            {u.id === selfId && (
              <span className='text-muted-foreground'>({m.tokens.self})</span>
            )}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
