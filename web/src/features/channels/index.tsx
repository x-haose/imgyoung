import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Plus, TriangleAlert } from 'lucide-react'
import { useMessages } from '@/context/locale-provider'
import { Page } from '@/components/layout/page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { QueryState } from '@/components/query-state'
import { ChannelSheet } from './channel-sheet'
import { ChannelTable } from './channel-table'
import { channelsQueryOptions } from './data'
import { DeleteChannelDialog, RemoveNowDialog } from './delete-dialog'
import { ExportConfigDialog } from './export-dialog'
import { type ChannelItem } from './types'

/** 存储渠道页:列表 + 模板化创建 / 编辑 + 手动探测 + 导出 rclone 配置 + 删除(可选清理远端对象)。 */
export function Channels() {
  const m = useMessages()
  const list = useQuery(channelsQueryOptions())
  const [sheet, setSheet] = useState<{
    open: boolean
    channel: ChannelItem | null
  }>({ open: false, channel: null })
  const [removing, setRemoving] = useState<ChannelItem | null>(null)
  const [removingNow, setRemovingNow] = useState<ChannelItem | null>(null)
  const [exporting, setExporting] = useState<ChannelItem | null>(null)
  const openCreate = () => setSheet({ open: true, channel: null })

  return (
    <Page
      title={m.channels.title}
      description={m.channels.description}
      actions={
        <Button onClick={openCreate}>
          <Plus />
          {m.channels.create}
        </Button>
      }
    >
      <QueryState query={list}>
        {list.data && (
          <>
            {onlyLocalDisk(list.data) && (
              <Alert className='border-warning/40 bg-warning/10'>
                <TriangleAlert className='text-warning' />
                <AlertTitle>{m.channels.soloLocalTitle}</AlertTitle>
                <AlertDescription>{m.channels.soloLocalDesc}</AlertDescription>
              </Alert>
            )}
            <ChannelTable
              items={list.data}
              onCreate={openCreate}
              onEdit={(channel) => setSheet({ open: true, channel })}
              onExport={setExporting}
              onDelete={setRemoving}
              onRemoveNow={setRemovingNow}
            />
          </>
        )}
      </QueryState>

      <ChannelSheet
        open={sheet.open}
        channel={sheet.channel}
        onOpenChange={(open) => setSheet((s) => ({ ...s, open }))}
      />
      <DeleteChannelDialog
        key={removing?.id ?? 'none'}
        channel={removing}
        onClose={() => setRemoving(null)}
      />
      <RemoveNowDialog
        channel={removingNow}
        onClose={() => setRemovingNow(null)}
      />
      <ExportConfigDialog
        channel={exporting}
        onClose={() => setExporting(null)}
      />
    </Page>
  )
}

/** 在役渠道只剩本机磁盘一个:主机全损即图片与快照一起丢失,需要提醒再接一个渠道。 */
function onlyLocalDisk(items: ChannelItem[]): boolean {
  const inService = items.filter((c) => !c.deleting)
  return inService.length === 1 && inService[0].driver === 'local'
}
