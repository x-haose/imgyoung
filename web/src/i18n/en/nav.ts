import type { nav as zhNav, navGroups as zhNavGroups } from '../zh-CN/nav'

export const navGroups = {
  content: 'Content',
  site: 'Site',
  other: 'Other',
} satisfies typeof zhNavGroups

export const nav = {
  dashboard: 'Dashboard',
  images: 'Images',
  imports: 'Import',
  tokens: 'Upload tokens',
  channels: 'Storage channels',
  users: 'Users',
  dedup: 'Duplicates',
  cleanup: 'Cleanup',
  audit: 'Audit log',
  guide: 'Guide',
  settings: 'Settings',
} satisfies typeof zhNav
