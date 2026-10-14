import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { t } from '@/i18n'
import {
  ArrowRight,
  CopyCheck,
  Eye,
  HardDrive,
  Images,
  Trash2,
  TriangleAlert,
  Users,
} from 'lucide-react'
import { formatBytes, formatNumber } from '@/lib/format'
import { useMessages } from '@/context/locale-provider'
import { Page } from '@/components/layout/page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { QueryState } from '@/components/query-state'
import { AlertsCard } from './alerts-card'
import { ChannelsCard } from './channels-card'
import {
  type AdminDashboard,
  dashboardQueryOptions,
  isAdminDashboard,
  type UserDashboard,
} from './data'
import { QuickStart } from './quick-start'
import { StatCard } from './stat-card'
import { TopImagesCard } from './top-images-card'

export function Dashboard() {
  const m = useMessages()
  const query = useQuery(dashboardQueryOptions())
  return (
    <Page title={m.dashboard.title}>
      <QueryState query={query} skeleton={<DashboardSkeleton />}>
        {query.data &&
          (isAdminDashboard(query.data) ? (
            <AdminView data={query.data} />
          ) : (
            <UserView data={query.data} />
          ))}
      </QueryState>
    </Page>
  )
}

function AdminView({ data }: { data: AdminDashboard }) {
  const m = useMessages()
  return (
    <div className='flex flex-col gap-6'>
      {data.backup_colocated && <BackupColocatedAlert />}
      {data.images === 0 && <QuickStart admin />}
      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
        <StatCard
          label={m.dashboard.images}
          value={formatNumber(data.images)}
          icon={Images}
          to='/images'
        />
        <StatCard
          label={m.dashboard.storage}
          value={formatBytes(data.storage_bytes)}
          hint={m.dashboard.storageHint}
          icon={HardDrive}
        />
        <StatCard
          label={m.dashboard.users}
          value={formatNumber(data.users)}
          icon={Users}
          to='/users'
        />
        <StatCard
          label={m.dashboard.accesses}
          value={formatNumber(data.total_accesses)}
          hint={`${t(m.dashboard.trafficEstimate, {
            bytes: formatBytes(data.traffic_estimate_bytes),
          })} · ${m.dashboard.accessesHint}`}
          icon={Eye}
        />
        <StatCard
          label={m.dashboard.pendingCleanup}
          value={formatNumber(data.pending_cleanup)}
          hint={m.dashboard.pendingCleanupHint}
          icon={Trash2}
          to='/cleanup'
          attention={data.pending_cleanup > 0}
        />
        <StatCard
          label={m.dashboard.pendingDedup}
          value={formatNumber(data.pending_dedup)}
          hint={m.dashboard.pendingDedupHint}
          icon={CopyCheck}
          to='/dedup'
          attention={data.pending_dedup > 0}
        />
      </div>
      <div className='grid gap-6 lg:grid-cols-5'>
        <ChannelsCard channels={data.channels} className='lg:col-span-3' />
        <AlertsCard className='lg:col-span-2' />
      </div>
      <TopImagesCard images={data.top_images} />
    </div>
  )
}

function UserView({ data }: { data: UserDashboard }) {
  const m = useMessages()
  const percent =
    data.quota_bytes === null || data.quota_bytes <= 0
      ? null
      : Math.min(100, Math.round((data.used_bytes / data.quota_bytes) * 100))
  return (
    <div className='flex flex-col gap-6'>
      {data.images === 0 && <QuickStart admin={false} />}
      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
        <StatCard
          label={m.dashboard.images}
          value={formatNumber(data.images)}
          icon={Images}
          to='/images'
        />
        <StatCard
          label={m.dashboard.used}
          value={formatBytes(data.used_bytes)}
          hint={
            percent === null
              ? undefined
              : t(m.dashboard.quotaUsage, { percent })
          }
          icon={HardDrive}
          attention={percent !== null && percent >= 90}
        />
        <StatCard
          label={m.dashboard.quota}
          value={
            data.quota_bytes === null
              ? m.dashboard.unlimited
              : formatBytes(data.quota_bytes)
          }
          icon={Users}
        />
      </div>
    </div>
  )
}

/** 快照与图片同盘的警示:主机全损会连同快照一起丢失。 */
function BackupColocatedAlert() {
  const m = useMessages()
  return (
    <Alert className='border-warning/40 bg-warning/10'>
      <TriangleAlert className='text-warning' />
      <AlertTitle>{m.dashboard.backupColocatedTitle}</AlertTitle>
      <AlertDescription>
        <p>{m.dashboard.backupColocatedDesc}</p>
        <Link
          to='/channels'
          className='inline-flex items-center gap-1 font-medium text-foreground underline-offset-4 hover:underline'
        >
          {m.dashboard.addChannel}
          <ArrowRight className='size-3.5' />
        </Link>
      </AlertDescription>
    </Alert>
  )
}

function DashboardSkeleton() {
  return (
    <div className='flex flex-col gap-6'>
      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className='h-24 rounded-xl' />
        ))}
      </div>
      <div className='grid gap-6 lg:grid-cols-5'>
        <Skeleton className='h-64 rounded-xl lg:col-span-3' />
        <Skeleton className='h-64 rounded-xl lg:col-span-2' />
      </div>
    </div>
  )
}
