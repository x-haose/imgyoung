import { Link } from '@tanstack/react-router'
import { BookOpen, KeyRound, Plus } from 'lucide-react'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'

type TokensEmptyStateProps = {
  viewingSelf: boolean // 看本人时给签发入口;看别人时只陈述
  onIssue: () => void
}

/** 空态指向下一步动作:签发第一个 Token,或去指南看客户端怎么接。 */
export function TokensEmptyState({
  viewingSelf,
  onIssue,
}: TokensEmptyStateProps) {
  const m = useMessages()
  return (
    <div className='flex flex-col items-center gap-4 py-6'>
      <div className='flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary'>
        <KeyRound className='size-6' />
      </div>
      <div className='space-y-1'>
        <p className='font-medium text-foreground'>
          {viewingSelf ? m.tokens.emptyTitle : m.tokens.emptyOther}
        </p>
        {viewingSelf && (
          <p className='text-sm text-muted-foreground'>{m.tokens.emptyDesc}</p>
        )}
      </div>
      <div className='flex flex-wrap justify-center gap-2'>
        {viewingSelf && (
          <Button onClick={onIssue}>
            <Plus />
            {m.tokens.issueFirst}
          </Button>
        )}
        <Button variant='outline' asChild>
          <Link to='/guide'>
            <BookOpen />
            {m.tokens.viewGuide}
          </Link>
        </Button>
      </div>
    </div>
  )
}
