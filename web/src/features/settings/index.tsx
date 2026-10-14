import { getRouteApi } from '@tanstack/react-router'
import { useMessages } from '@/context/locale-provider'
import { Page } from '@/components/layout/page'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AlertsTab } from './alerts-tab'
import { BackupTab } from './backup-tab'
import { ConfigTab } from './config-form'
import { RefererTab } from './referer-tab'
import { SETTINGS_TABS, type SettingsTab } from './tabs'

const route = getRouteApi('/_authenticated/settings/')

/** 设置页(管理员):四个分区由搜索参数 tab 记住,切换分区时丢弃分页参数。 */
export function Settings() {
  const m = useMessages()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const tab: SettingsTab = search.tab

  return (
    <Page title={m.settings.title} description={m.settings.description}>
      <Tabs
        value={tab}
        onValueChange={(value) =>
          navigate({ search: { tab: value as SettingsTab }, replace: true })
        }
      >
        <TabsList className='h-auto flex-wrap'>
          {SETTINGS_TABS.map((key) => (
            <TabsTrigger key={key} value={key}>
              {m.settings.tabs[key]}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value='config' className='mt-4'>
          <ConfigTab />
        </TabsContent>
        <TabsContent value='referer' className='mt-4'>
          <RefererTab />
        </TabsContent>
        <TabsContent value='backup' className='mt-4'>
          <BackupTab />
        </TabsContent>
        <TabsContent value='alerts' className='mt-4'>
          <AlertsTab />
        </TabsContent>
      </Tabs>
    </Page>
  )
}
