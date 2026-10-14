import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/errors'
import { passwordSchema } from '@/lib/password'
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
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { PasswordInput } from '@/components/password-input'
import { resetUserPassword, type UserItem } from './data'

type ResetPasswordDialogProps = {
  user: UserItem | null
  onClose: () => void
}

export function ResetPasswordDialog({
  user,
  onClose,
}: ResetPasswordDialogProps) {
  return (
    <Dialog open={user !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-sm'>
        {user && <ResetPasswordForm user={user} onDone={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

type Values = { password: string }

function ResetPasswordForm({
  user,
  onDone,
}: {
  user: UserItem
  onDone: () => void
}) {
  const m = useMessages()
  const schema = z.object({
    password: passwordSchema(m.auth, m.users.reset.passwordRequired),
  })
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { password: '' },
  })
  const reset = useMutation({
    mutationFn: (v: Values) => resetUserPassword(user.id, v.password),
    onSuccess: () => {
      toast.success(t(m.users.reset.done, { name: user.name }))
      onDone()
    },
    onError: (err) => form.setError('root', { message: errorMessage(err) }),
  })

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(m.users.reset.title, { name: user.name })}</DialogTitle>
        <DialogDescription>{m.users.reset.hint}</DialogDescription>
      </DialogHeader>
      <Form {...form}>
        <form
          id='reset-password'
          onSubmit={form.handleSubmit((v) => reset.mutate(v))}
          className='grid gap-4'
        >
          <FormField
            control={form.control}
            name='password'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{m.users.reset.password}</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoFocus
                    autoComplete='new-password'
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {form.formState.errors.root && (
            <p className='text-sm text-destructive'>
              {form.formState.errors.root.message}
            </p>
          )}
        </form>
      </Form>
      <DialogFooter>
        <Button type='button' variant='outline' onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button
          type='submit'
          form='reset-password'
          variant='destructive'
          disabled={reset.isPending}
        >
          {reset.isPending && <Loader2 className='animate-spin' />}
          {m.users.reset.confirm}
        </Button>
      </DialogFooter>
    </>
  )
}
