import { useServerConfig } from '@/hooks/use-server-config'

/** 侧栏底部的服务端版本号;配置未加载时不占位。鼠标悬停显示提交哈希。 */
export function BuildInfo() {
  const build = useServerConfig()?.build
  if (!build) return null
  return (
    <p
      className='truncate px-2 pb-1 font-mono text-[11px] text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden'
      title={build.commit}
    >
      {build.version}
    </p>
  )
}
