import { t } from '@/i18n'
import { useMessages } from '@/context/locale-provider'
import { useServerConfig } from '@/hooks/use-server-config'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { type FullConfig } from './data'
import { formatInstant } from './format'

/** 引导配置只读展示、服务端构建信息与内置策略:环境变量与二进制决定,改动需重启,这里只看不改。 */
export function BootstrapCard({ config }: { config: FullConfig }) {
  const b = useMessages().settings.config.bootstrap
  const { bootstrap, derived } = config
  const build = useServerConfig()?.build
  const rows: [string, string][] = [
    [b.listen, bootstrap.listen],
    [b.data_dir, bootstrap.data_dir],
    [b.trusted_proxies, bootstrap.trusted_proxies || b.trustNone],
    [b.metrics_allow, bootstrap.metrics_allow || b.metricsNone],
    [b.version, build?.version ?? b.unset],
    [b.commit, build?.commit || b.unset],
    [b.builtAt, build?.built_at ? formatInstant(build.built_at) : b.unset],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle>{b.title}</CardTitle>
        <CardDescription>{b.hint}</CardDescription>
      </CardHeader>
      <CardContent className='grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,20rem)]'>
        <dl className='grid gap-x-6 gap-y-4 sm:grid-cols-2'>
          {rows.map(([label, value]) => (
            <div key={label} className='grid gap-1'>
              <dt className='text-xs text-muted-foreground'>{label}</dt>
              <dd className='font-mono text-sm break-all'>{value}</dd>
            </div>
          ))}
        </dl>
        <div className='grid content-start gap-1.5 rounded-lg bg-muted/40 p-4 text-xs text-muted-foreground'>
          <span className='text-sm font-medium text-foreground'>
            {b.builtin}
          </span>
          <span>
            {t(b.loginRate, {
              burst: derived.login_burst,
              seconds: derived.login_refill_seconds,
            })}
          </span>
          <span>
            {t(b.reconcile, { seconds: derived.reconcile_interval_seconds })}
          </span>
        </div>
      </CardContent>
    </Card>
  )
}
