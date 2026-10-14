import { useState } from 'react'
import { ChevronsUpDown, KeyRound, LogOut } from 'lucide-react'
import { useSession } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { SignOutDialog } from '@/components/sign-out-dialog'
import { ChangePasswordForm } from '@/features/auth/change-password-form'

export function NavUser() {
  const { isMobile } = useSidebar()
  const { user } = useSession()
  const m = useMessages()
  const [signingOut, setSigningOut] = useState(false)
  const [changing, setChanging] = useState(false)

  const identity = (
    <>
      <Avatar className='size-8 rounded-lg'>
        <AvatarFallback className='rounded-lg bg-primary text-xs font-semibold text-primary-foreground'>
          {user.name.slice(0, 2).toUpperCase()}
        </AvatarFallback>
      </Avatar>
      <div className='grid flex-1 text-start text-sm leading-tight'>
        <span className='truncate font-semibold'>{user.name}</span>
        <span className='truncate text-xs opacity-70'>
          {m.auth.role[user.role]}
        </span>
      </div>
    </>
  )

  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <SidebarMenuButton
                size='lg'
                className='data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground'
              >
                {identity}
                <ChevronsUpDown className='ms-auto size-4' />
              </SidebarMenuButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              className='w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg'
              side={isMobile ? 'bottom' : 'right'}
              align='end'
              sideOffset={4}
            >
              <DropdownMenuLabel className='p-0 font-normal'>
                <div className='flex items-center gap-2 px-1 py-1.5 text-start text-sm'>
                  {identity}
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => setChanging(true)}>
                <KeyRound />
                {m.auth.changePassword}
              </DropdownMenuItem>
              <DropdownMenuItem
                variant='destructive'
                onClick={() => setSigningOut(true)}
              >
                <LogOut />
                {m.auth.signOut}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>

      <SignOutDialog open={signingOut} onOpenChange={setSigningOut} />
      <Dialog open={changing} onOpenChange={setChanging}>
        <DialogContent className='sm:max-w-sm'>
          <DialogHeader>
            <DialogTitle>{m.auth.changePassword}</DialogTitle>
            <DialogDescription>{m.auth.changePasswordNote}</DialogDescription>
          </DialogHeader>
          <ChangePasswordForm
            submitText={m.auth.changePassword}
            successMessage={m.auth.passwordChanged}
          />
        </DialogContent>
      </Dialog>
    </>
  )
}
