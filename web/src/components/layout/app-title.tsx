import { Link } from '@tanstack/react-router'
import { Menu, X } from 'lucide-react'
import { BrandMark } from '@/assets/brand-mark'
import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'

export function AppTitle() {
  const { setOpenMobile } = useSidebar()
  const m = useMessages()
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <SidebarMenuButton
          size='lg'
          className='gap-0 py-0 hover:bg-transparent active:bg-transparent'
          asChild
        >
          <div>
            <Link
              to='/'
              onClick={() => setOpenMobile(false)}
              className='flex flex-1 items-center gap-2.5 text-start text-sm leading-tight'
            >
              <BrandMark className='size-8' />
              <span className='grid group-data-[collapsible=icon]:hidden'>
                <span className='truncate font-semibold tracking-wide text-sidebar-accent-foreground'>
                  {m.common.appName}
                </span>
                <span className='truncate text-xs opacity-70'>
                  {m.common.appSubtitle}
                </span>
              </span>
            </Link>
            <ToggleSidebar />
          </div>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}

function ToggleSidebar({
  className,
  onClick,
  ...props
}: React.ComponentProps<typeof Button>) {
  const { toggleSidebar } = useSidebar()
  const m = useMessages()
  return (
    <Button
      data-sidebar='trigger'
      data-slot='sidebar-trigger'
      variant='ghost'
      size='icon'
      className={cn('aspect-square size-8 max-md:scale-125', className)}
      onClick={(event) => {
        onClick?.(event)
        toggleSidebar()
      }}
      {...props}
    >
      <X className='md:hidden' />
      <Menu className='max-md:hidden' />
      <span className='sr-only'>{m.common.toggleSidebar}</span>
    </Button>
  )
}
