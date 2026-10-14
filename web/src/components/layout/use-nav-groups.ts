import { useSession } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { sidebarData } from './data/sidebar-data'
import { type NavGroup } from './types'

/** 按当前角色筛过、标题已按当前语言解析的导航;筛空的分组不出现。 */
export function useNavGroups(): NavGroup[] {
  const { user } = useSession()
  const m = useMessages()
  return sidebarData.flatMap((group) => {
    const items = group.items
      .filter((item) => !item.requires || item.requires === user.role)
      .map((item) => ({
        title: m.nav[item.key],
        url: item.url,
        icon: item.icon,
      }))
    return items.length > 0 ? [{ title: m.navGroups[group.key], items }] : []
  })
}
