import { useQuery } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { ArrowLeft, PackageOpen } from 'lucide-react'
import { hasStatus } from '@/lib/errors'
import { formatRelativeTime } from '@/lib/format'
import { useSession } from '@/lib/session'
import { useMessages } from '@/context/locale-provider'
import { Page } from '@/components/layout/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { QueryState } from '@/components/query-state'
import { JobActions } from './actions'
import { importQueryOptions, type ImportJob, isRunning } from './data'
import { FailuresTable } from './failures-table'
import { HostPicker } from './host-picker'
import { ImportingPanel, ScanningPanel } from './progress-panels'
import { ResultPanel } from './result-panel'
import { ScanReport } from './scan-report'
import { SettingsCard } from './settings-card'
import { ImportStatusBadge } from './status-badge'

const route = getRouteApi('/_authenticated/imports/$id')

/** 任务详情页:按任务状态展示扫描进度、确认页、导入进度或结果;进行中的任务每 2 秒刷新。 */
export function ImportDetail() {
  const m = useMessages().imports
  const { id } = route.useParams()
  const query = useQuery(importQueryOptions(id))
  const job = query.data
  const { user } = useSession()

  return (
    <Page
      title={m.detail.title}
      description={
        job && (
          <span className='inline-flex flex-wrap items-center gap-x-2 gap-y-1'>
            <ImportStatusBadge status={job.status} />
            {m.mode[job.mode]} · {formatRelativeTime(job.created_at)}
            {job.user_id !== user.id &&
              ` · ${m.detail.owner}：${job.user_name}`}
          </span>
        )
      }
      actions={
        <>
          <Button variant='ghost' asChild>
            <Link to='/imports'>
              <ArrowLeft />
              {m.back}
            </Link>
          </Button>
          {job && <JobActions job={job} isOwner={job.user_id === user.id} />}
        </>
      }
    >
      {hasStatus(query.error, 404) ? (
        <NotFound />
      ) : (
        <QueryState query={query}>{job && <JobBody job={job} />}</QueryState>
      )}
    </Page>
  )
}

/** 任务不存在或不属于当前账号(两者对外都是 404)。 */
function NotFound() {
  const m = useMessages().imports.detail
  return (
    <Card>
      <CardContent className='flex flex-col items-center gap-3 py-12 text-center'>
        <PackageOpen className='size-8 text-muted-foreground/60' />
        <p className='font-medium'>{m.notFoundTitle}</p>
        <p className='text-sm text-muted-foreground'>{m.notFoundDesc}</p>
      </CardContent>
    </Card>
  )
}

/** 按状态选择主体:扫描中、待确认、导入中、结束。 */
function JobBody({ job }: { job: ImportJob }) {
  const isOwner = job.user_id === useSession().user.id
  const report = job.scan.report
  return (
    <div className='flex flex-col gap-6'>
      {job.status === 'scanning' && <ScanningPanel job={job} />}
      {job.status === 'ready' && <ReadyPanel job={job} isOwner={isOwner} />}
      {job.status === 'importing' && <ImportingPanel job={job} />}
      {!isRunning(job.status) && job.status !== 'ready' && (
        <ResultPanel job={job} />
      )}
      {job.counts.failed > 0 && (
        <FailuresTable id={job.id} live={isRunning(job.status)} />
      )}
      {report && <ScanReport report={report} />}
      <SettingsCard job={job} />
    </div>
  )
}

/** 待确认:有候选图片时选域名,什么也没找到时只陈述并等待删除。 */
function ReadyPanel({ job, isOwner }: { job: ImportJob; isOwner: boolean }) {
  const m = useMessages().imports.detail
  if (job.counts.candidate > 0) {
    return <HostPicker id={job.id} canStart={isOwner} />
  }
  return (
    <Card>
      <CardContent className='flex flex-col items-center gap-2 py-12 text-center'>
        <PackageOpen className='size-8 text-muted-foreground/60' />
        <p className='font-medium'>{m.noImages}</p>
        <p className='text-sm text-muted-foreground'>{m.noImagesDesc}</p>
      </CardContent>
    </Card>
  )
}
