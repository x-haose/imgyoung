import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { t } from '@/i18n'
import { Loader2, LogIn } from 'lucide-react'
import { errorMessage } from '@/lib/errors'
import {
  DEFAULT_ADMIN_PASSWORD,
  FIRST_RUN_PATH,
  login,
  safeRedirect,
  sessionQueryKey,
} from '@/lib/session'
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
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/password-input'
import { AuthLayout } from './auth-layout'

const route = getRouteApi('/(auth)/login')

type Values = { name: string; password: string }

export function Login() {
  const m = useMessages()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { redirect: redirectTo } = route.useSearch()
  const schema = z.object({
    name: z.string().min(1, m.auth.usernameRequired),
    password: z.string().min(1, m.auth.passwordRequired),
  })
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', password: '' },
  })

  const signIn = useMutation({
    mutationFn: (v: Values) => login(v.name, v.password),
    onSuccess: (session) => {
      // 换了账号的缓存一律作废,再放入新会话。
      queryClient.clear()
      queryClient.setQueryData(sessionQueryKey, session)
      if (session.user.must_change_password) {
        navigate({ to: FIRST_RUN_PATH, replace: true })
      } else {
        navigate({ href: safeRedirect(redirectTo), replace: true })
      }
    },
    onError: (err) => form.setError('root', { message: errorMessage(err) }),
  })

  return (
    <AuthLayout title={m.auth.loginTitle} subtitle={m.common.appSubtitle}>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit((v) => signIn.mutate(v))}
          className='grid gap-4'
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{m.auth.username}</FormLabel>
                <FormControl>
                  <Input autoComplete='username' autoFocus {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='password'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{m.auth.password}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete='current-password' {...field} />
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
          <Button className='mt-2 w-full' disabled={signIn.isPending}>
            {signIn.isPending ? (
              <Loader2 className='animate-spin' />
            ) : (
              <LogIn />
            )}
            {m.auth.login}
          </Button>
        </form>
      </Form>
      <p className='mt-5 text-center text-xs leading-relaxed text-muted-foreground'>
        {t(m.auth.loginHint, { password: DEFAULT_ADMIN_PASSWORD })}
        <br />
        {m.auth.forgotHint}
      </p>
    </AuthLayout>
  )
}
