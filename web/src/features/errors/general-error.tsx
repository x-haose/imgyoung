import { useNavigate, useRouter } from '@tanstack/react-router'
import { errorMessage } from '@/lib/errors'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'

type GeneralErrorProps = {
  error?: unknown
}

export function GeneralError({ error }: GeneralErrorProps) {
  const navigate = useNavigate()
  const router = useRouter()
  const m = useMessages()
  return (
    <div className='h-svh w-full'>
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        <span className='text-lg font-medium'>{m.errors.loadErrorTitle}</span>
        <p className='text-center text-muted-foreground'>
          {errorMessage(error)}
        </p>
        <div className='mt-6 flex gap-4'>
          <Button variant='outline' onClick={() => router.invalidate()}>
            {m.common.retry}
          </Button>
          <Button onClick={() => navigate({ to: '/' })}>{m.common.home}</Button>
        </div>
      </div>
    </div>
  )
}
