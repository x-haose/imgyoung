import { useNavigate, useRouter } from '@tanstack/react-router'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'

export function NotFoundError() {
  const navigate = useNavigate()
  const { history } = useRouter()
  const m = useMessages()
  return (
    <div className='h-svh'>
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        <h1 className='text-[7rem] leading-tight font-bold'>404</h1>
        <span className='font-medium'>{m.errors.notFoundTitle}</span>
        <p className='text-center text-muted-foreground'>
          {m.errors.notFoundDesc}
        </p>
        <div className='mt-6 flex gap-4'>
          <Button variant='outline' onClick={() => history.go(-1)}>
            {m.common.back}
          </Button>
          <Button onClick={() => navigate({ to: '/' })}>{m.common.home}</Button>
        </div>
      </div>
    </div>
  )
}
