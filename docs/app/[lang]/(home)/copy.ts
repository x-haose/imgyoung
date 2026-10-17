import type { FailoverDemoText } from '@/components/home/failover-demo'
import type { Language } from '@/lib/i18n'

/** 首页文案的结构;中英两份同型,缺项编译期报错。 */
type HomeCopy = {
  metaTitle: string
  metaDescription: string
  eyebrow: string
  headline: string
  lede: string
  start: string
  install: string
  demo: FailoverDemoText
  featuresTitle: string
  features: { title: string; body: string }[]
  backendsTitle: string
  backendsLede: string
  /** 渠道模板名,与该语言后台「新建渠道」里的模板名逐字一致;模板之外的 rclone 后端走自定义模式。 */
  backends: string[]
  backendsMore: string
  installTitle: string
  installLede: string
  installTabs: { label: string; lang: string; code: string }[]
  installMore: string
  strip: string[]
}

/** 安装命令,中英文共用。 */
const COMMANDS = {
  script: 'curl -fsSL https://raw.githubusercontent.com/x-haose/imgyoung/main/install.sh | sh',
  docker:
    'docker run -d --name imgyoung --restart unless-stopped \\\n  -p 8721:8721 -v imgyoung-data:/data haose/imgyoung:latest',
  compose:
    'curl -fsSLO https://raw.githubusercontent.com/x-haose/imgyoung/main/deploy/compose.yaml\ndocker compose up -d',
  library:
    'go get github.com/x-haose/imgyoung/store/rclone\ngo get github.com/x-haose/imgyoung/store/failover',
}

export const COPY: Record<Language, HomeCopy> = {
  zh: {
    metaTitle: '图样 ImgYoung — 多渠道主备容灾的自部署图床',
    metaDescription:
      '自部署图床：一张图同时存进多个存储渠道，主渠道故障时上传与访问自动切换；渠道商只见密文，元数据库自动备份到渠道。单二进制，Docker 一行起。',
    eyebrow: '开源 · MIT · 自部署',
    headline: '一张图，存进多个渠道',
    lede: '上传先写进优先级最高的健康渠道即返回，其余渠道在后台补齐副本；某个渠道挂了，访问立刻切到下一个有副本的渠道。渠道可以加密到只剩密文，元数据库按时快照到渠道里。只要另行备份了主密钥（异地渠道全部加密时，再备好各渠道导出的 rclone 配置），本机损坏后，图片与元数据都能从异地渠道找回。',
    start: '快速开始',
    install: '安装方式',
    demo: {
      visitor: '访问者',
      primary: '主 · R2',
      backups: ['备 · S3', '备 · WebDAV'],
      down: '不可用',
      serving: '服务中',
      fail: '模拟主渠道故障',
      recover: '恢复主渠道',
      healthyCaption: '正常时由优先级最高的健康渠道出图，另外两个渠道各存一份副本。',
      failedCaption:
        '主渠道不可用：访问即时切到下一个有副本的渠道，新上传顺延写进备份渠道；主渠道恢复后，缺的副本由后台补齐。',
    },
    featuresTitle: '稳定性第一',
    features: [
      {
        title: '多渠道主备容灾',
        body: '优先级表达主备，健康探测自动降级；上传失败顺延下一渠道，可要求同步写够 N 份；副本补齐任务持久化，进程重启不丢。',
      },
      {
        title: '渠道存储加密',
        body: '基于 rclone crypt，内容与文件名都加密，渠道商只见密文；密钥由主密钥派生，标准 rclone 工具即可自行解密。',
      },
      {
        title: '元数据库自动备份',
        body: '定时快照写到每个健康渠道，各渠道各自保留最近 N 份；后台可浏览快照并从中恢复。',
      },
      {
        title: '生命周期',
        body: '有效期三层可设、访问即续期；长期无人访问的冷图进待清理列表，由管理员决定删留。',
      },
      {
        title: '后台查重',
        body: '字节完全相同的图自动合并成一份；感知哈希相似的图并排给管理员裁决，上传路径不受影响。',
      },
      {
        title: '多种上传入口',
        body: 'HTTP API、PicGo / PicList、S3 协议（PicList 内置，PicGo 装插件即可）、URL 转存与后台拖拽粘贴，共用一条上传管道。',
      },
      {
        title: '按需处理',
        body: '?w= 按预设宽度缩放，按浏览器能力返回 WebP，结果缓存在本地磁盘；原图 URL 不变，适合交给 CDN 长期缓存。',
      },
      {
        title: '多用户与权限',
        body: '管理员建号、独立配额与上传频控；每个客户端一枚具名 Token，可过期、可轮转、可吊销，泄露止损只影响上传。',
      },
    ],
    backendsTitle: '存储渠道',
    backendsLede:
      '渠道就是一个 rclone 后端实例。常用的对象存储与网盘有现成模板，填几个字段就能接入；模板之外的后端在自定义模式里直接填 rclone 参数。',
    backends: [
      'Amazon S3',
      'Cloudflare R2',
      '阿里云 OSS',
      '腾讯云 COS',
      'Backblaze B2',
      'MinIO / 自建',
      'WebDAV',
      'OneDrive',
      'Google Drive',
      'Dropbox',
      'pCloud',
      'Box',
      'Yandex Disk',
    ],
    backendsMore: '以及 rclone 支持的其他后端',
    installTitle: '一条命令装好',
    installLede:
      '首次启动无需任何配置：浏览器打开 8721 端口，用 admin 和启动日志里打印的初始密码登录并改密。',
    installTabs: [
      { label: '一行脚本', lang: 'bash', code: COMMANDS.script },
      { label: 'Docker', lang: 'bash', code: COMMANDS.docker },
      { label: 'Compose', lang: 'bash', code: COMMANDS.compose },
      { label: 'Go 库', lang: 'bash', code: COMMANDS.library },
    ],
    installMore: '全部安装方式 →',
    strip: [
      '单二进制 · 纯 Go',
      'Linux / macOS / Windows',
      'amd64 / arm64',
      'OpenAPI 3.1 接口文档',
      'MIT 开源',
    ],
  },
  en: {
    metaTitle: 'ImgYoung — self-hosted image hosting with multi-channel failover',
    metaDescription:
      'Self-hosted image hosting that stores every image in several storage channels and fails over automatically when one goes down. Channels can see only ciphertext, and the metadata database backs itself up to them. One binary, one Docker command.',
    eyebrow: 'Open source · MIT · Self-hosted',
    headline: 'One image, many channels',
    lede: 'An upload returns as soon as the highest-priority healthy channel has it; the other channels receive replicas in the background. When a channel goes down, reads switch to the next one that holds a replica. Channels can be encrypted down to ciphertext, and the metadata database snapshots itself into them. As long as the master key is backed up separately (plus the exported rclone config of each channel if every off-site channel is encrypted), the images and the metadata can be recovered from the off-site channels if the server is lost.',
    start: 'Quick start',
    install: 'Install',
    demo: {
      visitor: 'Visitor',
      primary: 'R2 · primary',
      backups: ['S3 · backup', 'WebDAV · backup'],
      down: 'down',
      serving: 'serving',
      fail: 'Take the primary down',
      recover: 'Bring the primary back',
      healthyCaption:
        'Normally the highest-priority healthy channel serves the image; the other two each hold a replica.',
      failedCaption:
        'Primary down: reads switch at once to the next channel with a replica and new uploads go to the backups; once the primary recovers, background replication fills in what it missed.',
    },
    featuresTitle: 'Reliability first',
    features: [
      {
        title: 'Multi-channel failover',
        body: 'Priority defines primary and backups, and health checks demote failing channels. A failed upload moves on to the next channel, you can require N synchronous copies, and replication jobs survive restarts.',
      },
      {
        title: 'Channel encryption',
        body: 'Built on rclone crypt: content and file names are encrypted, so the provider sees only ciphertext. Keys derive from the master key, and stock rclone can decrypt the data on its own.',
      },
      {
        title: 'Automatic metadata backups',
        body: 'Scheduled snapshots are written to every healthy channel, each keeping its own latest N. Browse and restore them in the admin console.',
      },
      {
        title: 'Lifecycle',
        body: 'Expiry at three levels with renew-on-access. Images nobody has opened for a long time land in a cleanup queue, and an admin decides what goes.',
      },
      {
        title: 'Deduplication',
        body: 'Byte-identical images merge automatically; perceptually similar ones are shown side by side for an admin to judge. The upload path never waits on it.',
      },
      {
        title: 'Many ways to upload',
        body: 'HTTP API, PicGo / PicList, the S3 protocol (built into PicList, one plugin away in PicGo), URL import, and drag-and-drop in the admin console — all through one pipeline.',
      },
      {
        title: 'On-demand processing',
        body: 'Resize with ?w= to preset widths and serve WebP to browsers that accept it, cached on local disk. Original URLs never change, so a CDN can cache them for a year.',
      },
      {
        title: 'Users and permissions',
        body: 'Admins create accounts with their own quotas and upload rate limits. Each client gets a named token that can expire, rotate and be revoked; a leaked token can only upload.',
      },
    ],
    backendsTitle: 'Storage channels',
    backendsLede:
      'A channel is an rclone backend instance. Popular object stores and cloud drives come with templates that need only a few fields; anything else rclone supports works in custom mode with raw rclone parameters.',
    backends: [
      'Amazon S3',
      'Cloudflare R2',
      'Alibaba Cloud OSS',
      'Tencent Cloud COS',
      'Backblaze B2',
      'MinIO / self-hosted',
      'WebDAV',
      'OneDrive',
      'Google Drive',
      'Dropbox',
      'pCloud',
      'Box',
      'Yandex Disk',
    ],
    backendsMore: 'and any other backend rclone supports',
    installTitle: 'Install with one command',
    installLede:
      'No configuration on first start: open port 8721, sign in as admin with the initial password printed in the startup log, and set a new password.',
    installTabs: [
      { label: 'Script', lang: 'bash', code: COMMANDS.script },
      { label: 'Docker', lang: 'bash', code: COMMANDS.docker },
      { label: 'Compose', lang: 'bash', code: COMMANDS.compose },
      { label: 'Go library', lang: 'bash', code: COMMANDS.library },
    ],
    installMore: 'All install options →',
    strip: [
      'Single binary · pure Go',
      'Linux / macOS / Windows',
      'amd64 / arm64',
      'OpenAPI 3.1 reference',
      'MIT licensed',
    ],
  },
}
