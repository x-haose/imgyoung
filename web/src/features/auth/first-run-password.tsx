import { t } from '@/i18n'
import { TriangleAlert } from 'lucide-react'
import { DEFAULT_ADMIN_PASSWORD, useSession } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { AuthLayout } from './auth-layout'
import { ChangePasswordForm } from './change-password-form'

/** 首次强制改密页:仍用出厂默认密码的用户被路由守卫送到这里,改掉前不放行其余页面。 */
export function FirstRunPassword() {
  const m = useMessages()
  const { user } = useSession()
  return (
    <AuthLayout
      title={m.auth.firstRunTitle}
      subtitle={t(m.auth.firstRunSubtitle, { name: user.name })}
    >
      <Alert className='mb-5 border-warning/40 bg-warning/10'>
        <TriangleAlert className='text-warning' />
        <AlertTitle>{m.auth.firstRunWarningTitle}</AlertTitle>
        <AlertDescription>{m.auth.firstRunWarningDesc}</AlertDescription>
      </Alert>
      <ChangePasswordForm
        fixedOldPassword={DEFAULT_ADMIN_PASSWORD}
        submitText={m.auth.firstRunSubmit}
        successMessage={m.auth.firstRunDone}
      />
    </AuthLayout>
  )
}
