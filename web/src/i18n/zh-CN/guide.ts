/** 使用指南页文案:接入步骤、各客户端配置、链接规则与错误码速查。 */
export const guide = {
  title: '使用指南',
  description:
    '把图床接入博客、笔记与截图工具；本页的地址按当前站点生成，可直接复制。',
  sections: {
    quickStart: '快速开始',
    curl: 'curl / 脚本上传',
    picgo: 'PicGo / PicList 配置',
    s3: 'S3 兼容接口',
    urlFetch: '从图片地址转存',
    access: '访问链接与缩放',
    errors: '上传接口错误码',
    webUpload: '后台直接上传',
  },
  step1Title: '确认存储渠道',
  step1Desc:
    '系统已内置本地渠道 local，无需配置即可上传；要使用 S3、R2 等云存储，请在存储渠道页新建渠道（仅管理员）。',
  step2Title: '签发上传 Token',
  step2Desc:
    '在上传 Token 页为每个客户端签发一个具名 Token，明文只显示一次；在后台直接上传不需要 Token。',
  step3Title: '开始上传',
  step3Desc:
    '用 curl、PicGo 等客户端带 Token 调用上传接口，或直接在图片页上传。',
  goTo: {
    channels: '存储渠道',
    tokens: '上传 Token',
    images: '图片',
  },
  curlIntro: '任何能发 HTTP 请求的工具都能上传；Token 在上传 Token 页签发。',
  fields: {
    file: '必填，multipart 文件字段；按文件内容识别格式，仅支持 JPEG / PNG / GIF / WebP',
    ttl: '可选，有效期秒数；0 或 never 表示永久，省略时用你的默认有效期，未设置则用全局默认',
    exifPolicy:
      '可选，strip（剥离元数据）、passthrough（原样保留）或 spoof（换成假的机型、位置与时间），省略时用全局默认',
    idempotencyKey: '可选请求头，幂等键；重试时带同一个键，不会重复生成图片',
  },
  response: '成功响应（200）',
  picgoIntro:
    '使用 PicList 的「高级自定义」图床，或 PicGo 的 web-uploader 插件，按下表填写：',
  picgo: {
    apiUrl: 'API 地址',
    method: '请求方式',
    fileField: 'POST 参数名',
    headers: '自定义请求头',
    jsonPath: '响应 URL 路径（JSON 路径）',
  },
  baseUrlNote:
    '返回的 url 是绝对地址：设置了对外站点地址时用它，否则按请求的域名生成。经反向代理或 CDN 访问、对外域名与请求域名不同时，请在「设置 → 运行配置」填写对外站点地址（base_url）。',
  s3Intro:
    '本站也提供 S3 兼容接口：PicList 内置的「AWS S3」图床和装了 s3 插件的 PicGo 都能使用，Typora 经 PicGo 上传。在上传 Token 页打开某个 Token 的「S3 客户端配置」，按弹窗逐项填写。',
  s3: {
    credentials: '凭证',
    credentialsDesc:
      'access key 是 Token 的识别前缀，secret 由主密钥派生，可随时回后台查看；吊销 Token 后 S3 凭证同时失效',
    bucket: '桶名 / 区域',
    bucketDesc: '没有桶与区域的概念，填弹窗给出的默认值即可',
    style: '寻址风格',
    styleDesc: '必须使用路径风格（path-style）；客户端有这个开关时请打开',
    urls: '图片地址',
    urlsDesc:
      '由弹窗中的「自定义域名」加对象键组成，形如 /o/<用户 id>/<对象键>',
    ops: '支持的操作',
    opsDesc:
      'PutObject、HeadBucket、HeadObject、GetObject、DeleteObject；不支持列举对象、分片上传与 ACL',
  },
  s3Note:
    '同一对象键再次上传会按 S3 语义覆盖：该键改为指向新图，旧图不会删除，短链仍可访问。',
  urlFetchIntro:
    '提交一个图片地址，由服务端下载并保存，适合把其他图床上的图片迁移过来。图片页「上传图片」里的「链接转存」功能相同。',
  urlFetchFields: {
    url: '必填，图片地址，仅 http / https',
    fileName: '可选，覆盖由地址推断的文件名',
    ttlAndExif: '可选，与表单上传相同的 ttl 与 exif_policy',
  },
  urlFetchWarnTitle: '仅支持 http / https 的公网地址',
  urlFetchWarnDesc:
    '指向内网、环回或云元数据地址的请求会被拒绝，重定向的每一跳都会检查。',
  accessIntro:
    '每张图有一条短链；经 S3 客户端上传的图另有按对象键的别名，两者指向同一张图。',
  access: {
    shortLink: '短链',
    shortLinkDesc: '上传响应里的 url；图片删除或到期后立即失效',
    alias: '对象键别名',
    aliasDesc: '/o/<用户 id>/<对象键>，S3 客户端生成的链接可直接使用',
    width: '缩放',
    widthDesc:
      '加 ?w=400 按宽度缩放，可用宽度见「设置 → 运行配置」的「宽度档」（process_widths）；其他宽度返回 400',
    format: '格式',
    formatDesc:
      '不带 format 时按请求头 Accept 协商，浏览器通常得到 WebP；也可用 ?format=webp / jpeg / png 指定',
  },
  errorCols: {
    code: '状态码',
    meaning: '含义',
    advice: '处理建议',
  },
  errorRows: {
    400: {
      meaning:
        '请求有误：file 字段缺失、图片内容无法解析、ttl / exif_policy 取值非法',
      advice: '检查表单字段与取值格式',
    },
    401: {
      meaning: 'Token 无效、已吊销、已过期，或所属用户已被禁用',
      advice: '在上传 Token 页重新签发或轮转',
    },
    403: {
      meaning: 'Token 无上传权限，或存储配额已满',
      advice: '配额已满时删除不需要的图片，或请管理员调高配额',
    },
    413: {
      meaning: '文件超过单图大小上限',
      advice: '压缩后重试，或请管理员调大「单图上限」（max_upload_mb）',
    },
    415: {
      meaning: '不支持的文件类型',
      advice: '仅支持 JPEG / PNG / GIF / WebP，按文件内容识别，与扩展名无关',
    },
    422: {
      meaning: '图片分辨率超出像素预算',
      advice: '缩小分辨率后重试',
    },
    429: {
      meaning: '上传过于频繁，或已达上传并发上限',
      advice: '按响应头 Retry-After 给出的秒数等待后重试',
    },
    503: {
      meaning: '存储渠道暂不可用',
      advice: '稍后重试；持续出现时到渠道页检查健康状态',
    },
  },
  webUploadIntro:
    '在图片页点「上传图片」即可上传，使用当前登录的账号，不需要 Token；上传完成后可直接复制每张图的链接。',
}
