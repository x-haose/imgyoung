import type { components } from '@/lib/api.gen'

type Schemas = components['schemas']

/** 模板字段的显示文案;占位示例与帮助说明可缺省。 */
export type FieldText = { label: string; placeholder?: string; help?: string }

/**
 * 渠道模板的显示文案。服务端只下发结构,文案按模板 id、字段 key 与选项值在这里查;
 * 三类键都是接口文档里的枚举,漏写任何一项都过不了类型检查。
 */
export const channelTemplates = {
  /** 模板名与一句话说明。 */
  templates: {
    'aws-s3': { name: 'Amazon S3', description: 'AWS 官方对象存储' },
    r2: {
      name: 'Cloudflare R2',
      description: 'Cloudflare 对象存储,零出口流量费',
    },
    cos: { name: '腾讯云 COS', description: '腾讯云对象存储' },
    oss: { name: '阿里云 OSS', description: '阿里云对象存储' },
    b2: { name: 'Backblaze B2', description: 'Backblaze B2(S3 兼容接口)' },
    minio: { name: 'MinIO / 自建', description: 'MinIO 或其他自建 S3 服务' },
    's3-compatible': {
      name: 'S3 兼容(通用)',
      description: '任意兼容 S3 协议的服务',
    },
    webdav: {
      name: 'WebDAV',
      description: '坚果云、Nextcloud、群晖、Alist 等',
    },
    onedrive: { name: 'OneDrive', description: '微软 OneDrive 与 SharePoint' },
    googledrive: { name: 'Google Drive', description: '谷歌云端硬盘' },
    dropbox: { name: 'Dropbox', description: 'Dropbox 网盘' },
    pcloud: { name: 'pCloud', description: 'pCloud 网盘' },
    box: { name: 'Box', description: 'Box 企业网盘' },
    yandex: { name: 'Yandex Disk', description: 'Yandex 网盘' },
  } satisfies Record<
    Schemas['ChannelTemplateID'],
    { name: string; description: string }
  >,
  /** 字段的通用文案;同一字段在个别模板里另有说法的,见 overrides。 */
  fields: {
    access_key_id: { label: 'Access Key' },
    secret_access_key: { label: 'Secret Key' },
    region: { label: '区域' },
    bucket: { label: '存储桶', help: '存储桶名称,可加 / 前缀限定子目录' },
    public_base_url: {
      label: '公开访问地址',
      placeholder: 'https://cdn.example.com',
      help: '可选;存储桶(或其 CDN)的公开访问 URL 前缀,配置后该渠道可 302 直链',
    },
    account_id: {
      label: '账户 ID',
      placeholder: '如 a1b2c3d4e5f6',
      help: 'Cloudflare 账户 ID,用于拼接 R2 端点',
    },
    endpoint: { label: '服务端点', placeholder: 'https://minio.example.com' },
    url: {
      label: '服务地址',
      placeholder: 'https://dav.jianguoyun.com/dav/',
      help: 'WebDAV 根地址。坚果云为 https://dav.jianguoyun.com/dav/;Nextcloud/ownCloud 为 https://域名/remote.php/dav/files/用户名/;Alist 为 https://域名/dav/;群晖为 https://域名:5006/',
    },
    vendor: {
      label: '服务类型',
      help: '选错会导致列目录或分片上传行为异常;拿不准选「其他」',
    },
    user: { label: '用户名' },
    pass: {
      label: '密码',
      help: '坚果云须用「应用密码」而非登录密码,在其安全选项中生成',
    },
    client_id: {
      label: '应用 Client ID',
      help: '一键授权必填,填在服务商开放平台自建的应用标识;粘贴令牌时,若授权用的是自建应用也须填写。令牌续期用它,须与签发令牌的应用一致',
    },
    client_secret: {
      label: '应用 Client Secret',
      help: '与 Client ID 配套的应用密钥',
    },
    token: { label: '授权', help: '完成授权后自动填入,无需手工录入' },
    path: {
      label: '存储目录',
      placeholder: 'imgyoung',
      help: '网盘内用于存放图片的目录,留空存放于根目录',
    },
    hostname: {
      label: '数据区域',
      help: '须与注册账号时所选区域一致,否则换取令牌失败',
    },
  } satisfies Record<Schemas['TemplateFieldKey'], FieldText>,
  /** 模板专属的字段文案,逐项覆盖通用文案。 */
  overrides: {
    cos: {
      access_key_id: { label: 'SecretId' },
      secret_access_key: { label: 'SecretKey' },
      bucket: { help: '桶名需带 APPID 后缀,如 mybucket-1250000000' },
    },
    b2: {
      access_key_id: { label: 'Key ID' },
      secret_access_key: { label: 'Application Key' },
      region: {
        placeholder: '如 us-west-004',
        help: 'B2 账户所在区域,须与端点一致',
      },
    },
    's3-compatible': {
      endpoint: { label: 'S3 API 端点', placeholder: 'https://s3.example.com' },
    },
    onedrive: {
      region: { help: '决定授权与接口端点,须与账号所属区域一致' },
    },
  } satisfies Partial<
    Record<
      Schemas['ChannelTemplateID'],
      Partial<Record<Schemas['TemplateFieldKey'], Partial<FieldText>>>
    >
  >,
  /** 下拉选项的显示名,按选项值查;各字段的取值互不重复。 */
  options: {
    'us-east-1': '美国东部(弗吉尼亚北部)',
    'us-east-2': '美国东部(俄亥俄)',
    'us-west-1': '美国西部(加利福尼亚北部)',
    'us-west-2': '美国西部(俄勒冈)',
    'eu-west-1': '欧洲(爱尔兰)',
    'eu-central-1': '欧洲(法兰克福)',
    'ap-southeast-1': '亚太(新加坡)',
    'ap-northeast-1': '亚太(东京)',
    'ap-east-1': '亚太(香港)',
    'ap-southeast-2': '亚太(悉尼)',
    'ap-beijing': '北京',
    'ap-shanghai': '上海',
    'ap-guangzhou': '广州',
    'ap-chengdu': '成都',
    'ap-nanjing': '南京',
    'ap-hongkong': '中国香港',
    'ap-singapore': '新加坡',
    'ap-tokyo': '日本(东京)',
    'na-siliconvalley': '美国西部(硅谷)',
    'eu-frankfurt': '欧洲(法兰克福)',
    'oss-cn-hangzhou': '华东1(杭州)',
    'oss-cn-shanghai': '华东2(上海)',
    'oss-cn-beijing': '华北2(北京)',
    'oss-cn-zhangjiakou': '华北3(张家口)',
    'oss-cn-shenzhen': '华南1(深圳)',
    'oss-cn-guangzhou': '华南3(广州)',
    'oss-cn-hongkong': '中国香港',
    'oss-ap-southeast-1': '亚太东南1(新加坡)',
    'oss-ap-northeast-1': '亚太东北1(东京)',
    'oss-us-west-1': '美国西部1(硅谷)',
    'oss-eu-central-1': '欧洲中部1(法兰克福)',
    nextcloud: 'Nextcloud',
    owncloud: 'ownCloud 10',
    infinitescale: 'ownCloud Infinite Scale',
    sharepoint: 'SharePoint Online',
    'sharepoint-ntlm': 'SharePoint(NTLM 自建)',
    fastmail: 'Fastmail Files',
    rclone: 'rclone serve webdav',
    other: '其他(坚果云 / Alist / 群晖等)',
    global: '国际版',
    us: '美国政府云',
    de: '德国云',
    cn: '中国世纪互联',
    'api.pcloud.com': '美国',
    'eapi.pcloud.com': '欧洲',
  } satisfies Record<Schemas['TemplateOptionValue'], string>,
}
