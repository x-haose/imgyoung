import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { t } from '@/i18n'
import { Loader2 } from 'lucide-react'
import { errorMessage } from '@/lib/errors'
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
import {
  type IssuedToken,
  issueToken,
  TOKEN_EXPIRY_DAYS,
  TOKEN_NAME_MAX_LEN,
  tokensQueryKey,
} from './data'

type IssueTokenDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onIssued: (token: IssuedToken) => void // 签发成功,交给调用方展示一次性明文
}

/** 有效期选项值:小时数的字符串形态(Select 只收字符串),'0' 表示永久。 */
const expiryValues = ['0', ...TOKEN_EXPIRY_DAYS.map((d) => String(d * 24))]

type Values = { name: string; expires_in_hours: string }

export function IssueTokenDialog({
  open,
  onOpenChange,
  onIssued,
}: IssueTokenDialogProps) {
  const m = useMessages()
  const queryClient = useQueryClient()
  const schema = z.object({
    name: z
      .string()
      .trim()
      .min(1, m.tokens.create.nameRequired)
      .max(
        TOKEN_NAME_MAX_LEN,
        t(m.tokens.create.nameTooLong, { max: TOKEN_NAME_MAX_LEN })
      ),
    expires_in_hours: z.string(),
  })
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', expires_in_hours: '0' },
  })

  const issue = useMutation({
    mutationFn: (v: Values) =>
      issueToken({
        name: v.name,
        expires_in_hours: Number(v.expires_in_hours),
      }),
    onSuccess: (issued) => {
      queryClient.invalidateQueries({ queryKey: tokensQueryKey })
      form.reset()
      onOpenChange(false)
      onIssued(issued)
    },
    onError: (err) => form.setError('root', { message: errorMessage(err) }),
  })

  const expiryLabel = (value: string) =>
    value === '0'
      ? m.tokens.create.forever
      : t(m.tokens.create.days, { days: Number(value) / 24 })

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) form.reset()
        onOpenChange(next)
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{m.tokens.create.title}</DialogTitle>
          <DialogDescription>{m.tokens.description}</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form
            id='issue-token'
            onSubmit={form.handleSubmit((v) => issue.mutate(v))}
            className='grid gap-4'
          >
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{m.tokens.create.name}</FormLabel>
                  <FormControl>
                    <Input
                      autoFocus
                      maxLength={TOKEN_NAME_MAX_LEN}
                      placeholder={m.tokens.create.namePlaceholder}
                      className='font-mono'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>{m.tokens.create.nameHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='expires_in_hours'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{m.tokens.create.expiry}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {expiryValues.map((value) => (
                        <SelectItem key={value} value={value}>
                          {expiryLabel(value)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {m.tokens.create.expiryHint}
                  </FormDescription>
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
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {m.common.cancel}
          </Button>
          <Button type='submit' form='issue-token' disabled={issue.isPending}>
            {issue.isPending && <Loader2 className='animate-spin' />}
            {m.tokens.create.submit}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
