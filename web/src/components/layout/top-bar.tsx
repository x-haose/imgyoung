import { Header } from '@/components/layout/header'
import { LocaleSwitch } from '@/components/locale-switch'
import { Search } from '@/components/search'
import { ThemeSwitch } from '@/components/theme-switch'

/** 顶栏:侧栏开关、页面搜索与语言 / 主题切换,全站一份。 */
export function TopBar() {
  return (
    <Header fixed>
      <Search />
      <div className='ms-auto flex items-center gap-1'>
        <LocaleSwitch />
        <ThemeSwitch />
      </div>
    </Header>
  )
}
