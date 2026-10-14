import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { logout } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { ConfirmDialog } from '@/components/confirm-dialog'

type SignOutDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SignOutDialog({ open, onOpenChange }: SignOutDialogProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const m = useMessages()

  const signOut = useMutation({
    mutationFn: logout,
    onSuccess: () => {
      queryClient.clear()
      navigate({ to: '/login', replace: true })
    },
  })

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={m.auth.signOut}
      desc={m.auth.signOutConfirm}
      confirmText={m.auth.signOut}
      destructive
      isLoading={signOut.isPending}
      handleConfirm={() => signOut.mutate()}
      className='sm:max-w-sm'
    />
  )
}
