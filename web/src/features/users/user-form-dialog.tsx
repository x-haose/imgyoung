import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type Messages, t } from '@/i18n'
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
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { PasswordInput } from '@/components/password-input'
import {
  createUser,
  updateUser,
  USER_TTL_MAX_HOURS,
  type UserItem,
  type UserRole,
  usersQueryKey,
  type UserCreateBody,
  type UserUpdateBody,
} from './data'

/** 配额输入单位与字节换算。 */
const quotaUnits = { MB: 1 << 20, GB: 1 << 30 } as const
type QuotaUnit = keyof typeof quotaUnits

const roles: UserRole[] = ['user', 'admin']

/** 表单值一律用字符串承载数字输入,提交时再换算,避免受控输入框与数字类型互相打架。 */
type Values = {
  name: string
  password: string
  role: UserRole
  quota: string
  quotaUnit: QuotaUnit
  ttl: string
  disabled: boolean
}

function buildSchema(m: Messages, creating: boolean) {
  return z.object({
    name: z.string().trim().min(1, m.users.form.nameRequired),
    password: creating
      ? passwordSchema(m.auth, m.users.form.passwordRequired)
      : z.string(),
    role: z.enum(roles),
    quota: z
      .string()
      .trim()
      .refine(
        (v) => v === '' || /^\d+(\.\d+)?$/.test(v),
        m.users.form.quotaInvalid
      ),
    quotaUnit: z.enum(['MB', 'GB']),
    ttl: z
      .string()
      .trim()
      .refine(
        (v) => v === '' || (/^\d+$/.test(v) && Number(v) <= USER_TTL_MAX_HOURS),
        t(m.users.form.ttlInvalid, { max: USER_TTL_MAX_HOURS })
      ),
    disabled: z.boolean(),
  })
}

/** 已有用户 → 表单值:配额能整除 GB 就按 GB 显示,否则按 MB。 */
function valuesOf(user: UserItem | null): Values {
  const quota = user?.quota_bytes ?? 0
  const unit: QuotaUnit = quota > 0 && quota % quotaUnits.GB === 0 ? 'GB' : 'MB'
  return {
    name: user?.name ?? '',
    password: '',
    role: user?.role ?? 'user',
    quota: quota > 0 ? String(quota / quotaUnits[unit]) : '',
    quotaUnit: quota > 0 ? unit : 'GB',
    ttl:
      user?.default_ttl_hours === null || user === null
        ? ''
        : String(user.default_ttl_hours),
    disabled: user?.disabled ?? false,
  }
}

/** 表单值里两种请求体共有的部分;配额换算回字节,空有效期即随全局。 */
function sharedFields(v: Values) {
  const quota =
    v.quota === '' ? 0 : Math.round(Number(v.quota) * quotaUnits[v.quotaUnit])
  return {
    name: v.name,
    role: v.role,
    quota_bytes: quota,
    default_ttl_hours: v.ttl === '' ? null : Number(v.ttl),
  }
}

function createBody(v: Values): UserCreateBody {
  return { ...sharedFields(v), password: v.password }
}

function updateBody(v: Values): UserUpdateBody {
  return { ...sharedFields(v), disabled: v.disabled }
}

type UserFormDialogProps = {
  target: UserItem | 'new' | null // 'new' 建号;已有用户为编辑;null 关闭
  onClose: () => void
}

export function UserFormDialog({ target, onClose }: UserFormDialogProps) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-md'>
        {target !== null && (
          <UserForm user={target === 'new' ? null : target} onDone={onClose} />
        )}
      </DialogContent>
    </Dialog>
  )
}

/** 表单随弹窗打开才挂载,默认值按目标用户一次性算好,关闭即卸载不留脏状态。 */
function UserForm({
  user,
  onDone,
}: {
  user: UserItem | null
  onDone: () => void
}) {
  const m = useMessages()
  const queryClient = useQueryClient()
  const creating = user === null
  const form = useForm<Values>({
    resolver: zodResolver(buildSchema(m, creating)),
    defaultValues: valuesOf(user),
  })
  const save = useMutation({
    mutationFn: async (v: Values) => {
      if (creating) await createUser(createBody(v))
      else await updateUser(user.id, updateBody(v))
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: usersQueryKey })
      toast.success(creating ? m.users.form.created : m.users.form.saved)
      onDone()
    },
    onError: (err) => form.setError('root', { message: errorMessage(err) }),
  })

  return (
    <>
      <DialogHeader>
        <DialogTitle>
          {creating
            ? m.users.form.createTitle
            : t(m.users.form.editTitle, { name: user.name })}
        </DialogTitle>
        <DialogDescription>{m.users.description}</DialogDescription>
      </DialogHeader>
      <Form {...form}>
        <form
          id='user-form'
          onSubmit={form.handleSubmit((v) => save.mutate(v))}
          className='grid gap-4'
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{m.users.form.name}</FormLabel>
                <FormControl>
                  <Input
                    autoFocus
                    autoComplete='off'
                    className='font-mono'
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {creating && (
            <FormField
              control={form.control}
              name='password'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{m.users.form.password}</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete='new-password' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='role'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{m.users.form.role}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {roles.map((role) => (
                        <SelectItem key={role} value={role}>
                          {m.auth.role[role]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='quota'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{m.users.form.quota}</FormLabel>
                  <div className='flex gap-2'>
                    <FormControl>
                      <Input
                        inputMode='decimal'
                        placeholder='0'
                        className='tabular-nums'
                        {...field}
                      />
                    </FormControl>
                    <FormField
                      control={form.control}
                      name='quotaUnit'
                      render={({ field: unitField }) => (
                        <Select
                          value={unitField.value}
                          onValueChange={unitField.onChange}
                        >
                          <SelectTrigger className='w-20 shrink-0'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {Object.keys(quotaUnits).map((unit) => (
                              <SelectItem key={unit} value={unit}>
                                {unit}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      )}
                    />
                  </div>
                  <FormDescription>{m.users.form.quotaHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <FormField
            control={form.control}
            name='ttl'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{m.users.form.ttl}</FormLabel>
                <FormControl>
                  <Input
                    inputMode='numeric'
                    placeholder={m.users.form.ttlPlaceholder}
                    className='tabular-nums'
                    {...field}
                  />
                </FormControl>
                <FormDescription>{m.users.form.ttlHint}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {!creating && (
            <FormField
              control={form.control}
              name='disabled'
              render={({ field }) => (
                <FormItem className='flex items-center justify-between gap-4 rounded-lg border px-4 py-3'>
                  <div className='space-y-1'>
                    <FormLabel>{m.users.form.disabled}</FormLabel>
                    <FormDescription>
                      {m.users.form.disabledHint}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
          )}
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
        <Button type='submit' form='user-form' disabled={save.isPending}>
          {save.isPending && <Loader2 className='animate-spin' />}
          {m.users.form.submit}
        </Button>
      </DialogFooter>
    </>
  )
}
