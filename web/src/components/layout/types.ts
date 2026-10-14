import { type LinkProps } from '@tanstack/react-router'
import { type Messages } from '@/i18n'

export type NavKey = keyof Messages['nav']
export type NavGroupKey = keyof Messages['navGroups']
export type Role = 'admin' | 'user'

/** 导航项定义:标题是文案键,按当前语言解析;requires 指定只对该角色显示。 */
export type NavLinkDef = {
  key: NavKey
  url: LinkProps['to']
  icon: React.ElementType
  requires?: Role
}

export type NavGroupDef = {
  key: NavGroupKey
  items: NavLinkDef[]
}

/** 解析后的导航项:标题已按当前语言取值,只含当前角色可见的项。 */
export type NavLink = {
  title: string
  url: LinkProps['to']
  icon: React.ElementType
}

export type NavGroup = {
  title: string
  items: NavLink[]
}
