import { Link, useLocation } from '@tanstack/react-router'
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { type NavGroup as NavGroupProps, type NavLink } from './types'

export function NavGroup({ title, items }: NavGroupProps) {
  const pathname = useLocation({ select: (location) => location.pathname })
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{title}</SidebarGroupLabel>
      <SidebarMenu>
        {items.map((item) => (
          <SidebarMenuLink
            key={String(item.url)}
            item={item}
            active={isActive(pathname, String(item.url))}
          />
        ))}
      </SidebarMenu>
    </SidebarGroup>
  )
}

function SidebarMenuLink({ item, active }: { item: NavLink; active: boolean }) {
  const { setOpenMobile } = useSidebar()
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={active} tooltip={item.title}>
        <Link to={item.url} onClick={() => setOpenMobile(false)}>
          <item.icon />
          <span>{item.title}</span>
        </Link>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

/** 当前路径等于项地址或在其子路径下;总览只在根路径高亮。 */
function isActive(pathname: string, url: string): boolean {
  if (url === '/') return pathname === '/'
  return pathname === url || pathname.startsWith(`${url}/`)
}
