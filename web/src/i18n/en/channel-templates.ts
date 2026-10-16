import type { channelTemplates as zh } from '../zh-CN/channel-templates'

export const channelTemplates = {
  templates: {
    'aws-s3': { name: 'Amazon S3', description: 'Object storage from AWS' },
    r2: {
      name: 'Cloudflare R2',
      description: 'Cloudflare object storage with no egress fees',
    },
    cos: {
      name: 'Tencent Cloud COS',
      description: 'Tencent Cloud Object Storage',
    },
    oss: {
      name: 'Alibaba Cloud OSS',
      description: 'Alibaba Cloud Object Storage Service',
    },
    b2: {
      name: 'Backblaze B2',
      description: 'Backblaze B2 (S3-compatible API)',
    },
    minio: {
      name: 'MinIO / self-hosted',
      description: 'MinIO or another self-hosted S3 service',
    },
    's3-compatible': {
      name: 'S3-compatible (generic)',
      description: 'Any service with an S3-compatible API',
    },
    webdav: {
      name: 'WebDAV',
      description: 'Nutstore, Nextcloud, Synology, Alist and more',
    },
    onedrive: {
      name: 'OneDrive',
      description: 'Microsoft OneDrive and SharePoint',
    },
    googledrive: {
      name: 'Google Drive',
      description: "Google's cloud storage",
    },
    dropbox: { name: 'Dropbox', description: 'Dropbox cloud storage' },
    pcloud: { name: 'pCloud', description: 'pCloud cloud storage' },
    box: { name: 'Box', description: 'Box enterprise cloud storage' },
    yandex: { name: 'Yandex Disk', description: 'Yandex cloud storage' },
  },
  fields: {
    access_key_id: { label: 'Access key ID' },
    secret_access_key: { label: 'Secret access key' },
    region: { label: 'Region' },
    bucket: {
      label: 'Bucket',
      help: 'Bucket name; write bucket/prefix to keep objects in a subfolder',
    },
    public_base_url: {
      label: 'Public URL',
      placeholder: 'https://cdn.example.com',
      help: 'Optional. Public URL prefix of the bucket (or its CDN); once set, this channel can serve 302 direct links',
    },
    account_id: {
      label: 'Account ID',
      placeholder: 'e.g. a1b2c3d4e5f6',
      help: 'Your Cloudflare account ID, used to build the R2 endpoint',
    },
    endpoint: { label: 'Endpoint', placeholder: 'https://minio.example.com' },
    url: {
      label: 'Server URL',
      placeholder: 'https://dav.jianguoyun.com/dav/',
      help: 'WebDAV root URL. Nutstore: https://dav.jianguoyun.com/dav/; Nextcloud/ownCloud: https://<domain>/remote.php/dav/files/<username>/; Alist: https://<domain>/dav/; Synology: https://<domain>:5006/',
    },
    vendor: {
      label: 'Server type',
      help: 'A wrong choice can break directory listing or chunked uploads; if unsure, choose Other',
    },
    user: { label: 'Username' },
    pass: {
      label: 'Password',
      help: 'Nutstore requires an app password rather than your sign-in password; create one in its security settings',
    },
    client_id: {
      label: 'App client ID',
      help: "Required for one-click authorization: the ID of the app you created on the provider's developer platform. Also fill it in when the pasted token came from your own app. It must match the app that issued the token, or the token cannot be renewed",
    },
    client_secret: {
      label: 'App client secret',
      help: 'The secret that goes with the client ID',
    },
    token: {
      label: 'Authorization',
      help: 'Filled in automatically once you authorize; no need to type it',
    },
    path: {
      label: 'Folder',
      placeholder: 'imgyoung',
      help: 'Folder in the drive that holds the images; leave empty to use the root',
    },
    hostname: {
      label: 'Data region',
      help: 'Must match the region chosen when the account was created, otherwise the token exchange fails',
    },
  },
  overrides: {
    cos: {
      access_key_id: { label: 'SecretId' },
      secret_access_key: { label: 'SecretKey' },
      bucket: {
        help: 'The bucket name must include the APPID suffix, e.g. mybucket-1250000000',
      },
    },
    b2: {
      access_key_id: { label: 'Key ID' },
      secret_access_key: { label: 'Application key' },
      region: {
        placeholder: 'e.g. us-west-004',
        help: 'Region of your B2 account; must match the endpoint',
      },
    },
    's3-compatible': {
      endpoint: {
        label: 'S3 API endpoint',
        placeholder: 'https://s3.example.com',
      },
    },
    onedrive: {
      region: {
        help: 'Determines the authorization and API endpoints; must match the region of the account',
      },
    },
  },
  options: {
    'us-east-1': 'US East (N. Virginia)',
    'us-east-2': 'US East (Ohio)',
    'us-west-1': 'US West (N. California)',
    'us-west-2': 'US West (Oregon)',
    'eu-west-1': 'Europe (Ireland)',
    'eu-central-1': 'Europe (Frankfurt)',
    'ap-southeast-1': 'Asia Pacific (Singapore)',
    'ap-northeast-1': 'Asia Pacific (Tokyo)',
    'ap-east-1': 'Asia Pacific (Hong Kong)',
    'ap-southeast-2': 'Asia Pacific (Sydney)',
    'ap-beijing': 'North China (Beijing)',
    'ap-shanghai': 'East China (Shanghai)',
    'ap-guangzhou': 'South China (Guangzhou)',
    'ap-chengdu': 'Southwest China (Chengdu)',
    'ap-nanjing': 'East China (Nanjing)',
    'ap-hongkong': 'Hong Kong (China)',
    'ap-singapore': 'Southeast Asia (Singapore)',
    'ap-tokyo': 'Northeast Asia (Tokyo)',
    'na-siliconvalley': 'Western US (Silicon Valley)',
    'eu-frankfurt': 'Europe (Frankfurt)',
    'oss-cn-hangzhou': 'China (Hangzhou)',
    'oss-cn-shanghai': 'China (Shanghai)',
    'oss-cn-beijing': 'China (Beijing)',
    'oss-cn-zhangjiakou': 'China (Zhangjiakou)',
    'oss-cn-shenzhen': 'China (Shenzhen)',
    'oss-cn-guangzhou': 'China (Guangzhou)',
    'oss-cn-hongkong': 'China (Hong Kong)',
    'oss-ap-southeast-1': 'Singapore',
    'oss-ap-northeast-1': 'Japan (Tokyo)',
    'oss-us-west-1': 'US (Silicon Valley)',
    'oss-eu-central-1': 'Germany (Frankfurt)',
    nextcloud: 'Nextcloud',
    owncloud: 'ownCloud 10',
    infinitescale: 'ownCloud Infinite Scale',
    sharepoint: 'SharePoint Online',
    'sharepoint-ntlm': 'SharePoint (self-hosted, NTLM)',
    fastmail: 'Fastmail Files',
    rclone: 'rclone serve webdav',
    other: 'Other (Nutstore, Alist, Synology, …)',
    global: 'Global',
    us: 'Microsoft Cloud for US Government',
    de: 'Microsoft Cloud Germany',
    cn: 'China (operated by 21Vianet)',
    'api.pcloud.com': 'United States',
    'eapi.pcloud.com': 'Europe',
  },
} satisfies typeof zh
