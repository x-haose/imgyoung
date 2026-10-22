import {
  BookOpen,
  CopyCheck,
  HardDrive,
  Images,
  Import,
  KeyRound,
  LayoutDashboard,
  ScrollText,
  Settings,
  Trash2,
  Users,
} from 'lucide-react'
import { type NavGroupDef } from '../types'

/** 侧栏导航:内容区对全部登录用户开放,站点区与设置仅管理员可见。 */
export const sidebarData: NavGroupDef[] = [
  {
    key: 'content',
    items: [
      { key: 'dashboard', url: '/', icon: LayoutDashboard },
      { key: 'images', url: '/images', icon: Images },
      { key: 'imports', url: '/imports', icon: Import },
      { key: 'tokens', url: '/tokens', icon: KeyRound },
    ],
  },
  {
    key: 'site',
    items: [
      { key: 'channels', url: '/channels', icon: HardDrive, requires: 'admin' },
      { key: 'users', url: '/users', icon: Users, requires: 'admin' },
      { key: 'dedup', url: '/dedup', icon: CopyCheck, requires: 'admin' },
      { key: 'cleanup', url: '/cleanup', icon: Trash2, requires: 'admin' },
      { key: 'audit', url: '/audit', icon: ScrollText, requires: 'admin' },
    ],
  },
  {
    key: 'other',
    items: [
      { key: 'guide', url: '/guide', icon: BookOpen },
      { key: 'settings', url: '/settings', icon: Settings, requires: 'admin' },
    ],
  },
]
