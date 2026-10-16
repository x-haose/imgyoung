import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { type Messages } from '@/i18n'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { api, ApiError, setCsrfToken } from '@/lib/api'
import { errorMessage } from '@/lib/errors'
import { passwordSchema } from '@/lib/password'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { PasswordInput } from '@/components/password-input'

function buildSchema(m: Messages['auth'], firstRun: boolean) {
  return z
    .object({
      old_password: z
        .string()
        .min(1, firstRun ? m.initialPasswordRequired : m.oldPasswordRequired),
      new_password: passwordSchema(m, m.newPasswordRequired),
      confirm: z.string().min(1, m.confirmRequired),
    })
    .refine((v) => v.new_password === v.confirm, {
      path: ['confirm'],
      message: m.passwordMismatch,
    })
}

type Values = z.infer<ReturnType<typeof buildSchema>>

type ChangePasswordFormProps = {
  firstRun?: boolean // 首次改密:旧密码一栏标为「初始密码」
  submitText: string
  successMessage: string
}

/** 改密表单:成功后服务端已让全部会话失效,前端清缓存并回登录页。 */
export function ChangePasswordForm({
  firstRun = false,
  submitText,
  successMessage,
}: ChangePasswordFormProps) {
  const m = useMessages()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const form = useForm<Values>({
    resolver: zodResolver(buildSchema(m.auth, firstRun)),
    defaultValues: {
      old_password: '',
      new_password: '',
      confirm: '',
    },
  })

  const change = useMutation({
    mutationFn: (v: Values) =>
      api.put('/api/admin/me/password', {
        old_password: v.old_password,
        new_password: v.new_password,
      }),
    onSuccess: () => {
      toast.success(successMessage)
      setCsrfToken('')
      queryClient.clear()
      navigate({ to: '/login', replace: true })
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === 'old_password_incorrect') {
        form.setError('old_password', {
          message: firstRun ? m.auth.initialPasswordIncorrect : err.message,
        })
        return
      }
      form.setError('root', { message: errorMessage(err) })
    },
  })

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((v) => change.mutate(v))}
        className='grid gap-4'
      >
        <FormField
          control={form.control}
          name='old_password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {firstRun ? m.auth.initialPassword : m.auth.oldPassword}
              </FormLabel>
              <FormControl>
                <PasswordInput
                  autoComplete='current-password'
                  autoFocus
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='new_password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{m.auth.newPassword}</FormLabel>
              <FormControl>
                <PasswordInput autoComplete='new-password' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='confirm'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{m.auth.confirmPassword}</FormLabel>
              <FormControl>
                <PasswordInput autoComplete='new-password' {...field} />
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
        <Button className='mt-2 w-full' disabled={change.isPending}>
          {change.isPending && <Loader2 className='animate-spin' />}
          {submitText}
        </Button>
      </form>
    </Form>
  )
}
