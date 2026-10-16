/** 使用指南页文案:接入步骤、各客户端配置、链接规则与错误码速查。 */
export const guide = {
  title: '使用指南',
  description:
    '把图床接进博客、笔记与截图工作流；本页地址按当前站点生成，可直接复制。',
  sections: {
    quickStart: '快速开始',
    curl: 'curl / 脚本上传',
    picgo: 'PicGo / PicList 配置',
    s3: 'S3 协议客户端',
    urlFetch: '从图片地址转存',
    access: '访问链接与缩放',
    errors: '上传接口错误码',
    webUpload: '后台直接上传',
  },
  step1Title: '确认存储渠道',
  step1Desc:
    '已内置 local 本地渠道，开箱即用；要接 S3 / R2 等云存储，到存储渠道页新建（仅管理员）。',
  step2Title: '签发上传 Token',
  step2Desc:
    '到上传 Token 页给 PicGo、脚本各签发一枚具名 Token，明文只展示一次；后台直接上传不需要 Token。',
  step3Title: '开始上传',
  step3Desc: '用 curl / PicGo 带 Token 调上传接口，或直接在图片页上传。',
  goTo: {
    channels: '存储渠道',
    tokens: '上传 Token',
    images: '图片',
    settings: '设置',
  },
  curlIntro: '任何能发 HTTP 请求的工具都能上传；Token 在上传 Token 页签发。',
  fields: {
    file: '必填，multipart 文件字段；按内容识别格式，仅收 jpeg / png / gif / webp',
    ttl: '可选，有效期秒数；0 或 never 表示永久，留空跟随默认（用户默认有效期，未设置则全局默认）',
    exifPolicy:
      '可选，strip（剥离元数据）/ passthrough（原样保留），留空跟随全局默认',
    idempotencyKey: '可选请求头，幂等键；网络重试时带同一键不会重复建图',
  },
  response: '成功响应（200）',
  picgoIntro:
    '用 PicList 自带的「自定义图床」或 PicGo 的 web-uploader 插件，按下表逐项填写：',
  picgo: {
    apiUrl: 'API 地址',
    method: '请求方式',
    fileField: 'POST 参数名',
    headers: '自定义请求头',
    jsonPath: '响应 URL 路径（JSON 路径）',
  },
  baseUrlNote:
    '未配置对外站点地址时，返回的 url 按请求域名推导为绝对地址，直连场景开箱即用；经反代 / CDN 后对外域名与请求域名不一致时，到「设置 → 运行配置」把 base_url 填成你的对外域名。',
  s3Intro:
    '本站同时以 S3 协议收图：PicList 内置「AWS S3」图床，PicGo 装上 S3 插件后同样可用，Typora 经 PicGo 上传。到上传 Token 页点某枚 Token 的「S3 客户端配置」，照着弹窗逐项填写。',
  s3: {
    credentials: '凭证',
    credentialsDesc:
      'access key 是 Token 前缀，secret 由主密钥派生；可随时回后台重看。吊销 Token，S3 这条路同时失效',
    bucket: '桶名 / 区域',
    bucketDesc: '本站无桶概念也不分区域，按弹窗给的默认值填即可',
    style: '寻址风格',
    styleDesc: '路径风格（path-style）；客户端若有该开关请勾上',
    urls: '图片地址',
    urlsDesc: '按弹窗给的「自定义域名」拼，形如 /o/<用户 id>/<对象键>',
    ops: '支持的操作',
    opsDesc:
      'PutObject、HeadBucket、HeadObject、GetObject、DeleteObject；不提供列桶、分段上传与 ACL',
  },
  s3Note:
    '同一对象键重复上传按 S3 语义覆盖：该键指向新图，旧图的短链仍然有效。',
  urlFetchIntro:
    '给一个图片网址，服务端替你抓回来存进图床，用于把散在别人图床上的图收归自有。图片页的上传入口里也有同样的功能。',
  urlFetchFields: {
    url: '必填，图片地址，仅 http / https',
    fileName: '可选，覆盖由地址推断的文件名',
    ttlAndExif: '可选，与表单上传相同的 ttl 与 exif_policy',
  },
  urlFetchWarnTitle: '仅支持 http / https 的公网地址',
  urlFetchWarnDesc:
    '指向内网、环回或云元数据地址的请求一律拒绝（含重定向的每一跳），这是防止有人借本服务探测你的内网。',
  accessIntro:
    '每张图有一条短链；经 S3 客户端上传的图另有按对象键的别名，两者指向同一张图。',
  access: {
    shortLink: '短链',
    shortLinkDesc: '上传响应里的 url；删除图片后立即失效',
    alias: '对象键别名',
    aliasDesc: '/o/<用户 id>/<对象键>，S3 客户端贴出来的 Markdown 链接直接可用',
    width: '缩放',
    widthDesc:
      '加 ?w=400 按宽度档缩放，档位由「设置 → 运行配置」的 process_widths 决定；不在档位内的宽度返回 400',
    format: '格式',
    formatDesc:
      '不带参数时按浏览器 Accept 自动给 WebP；?format=webp / jpeg / png 可显式指定',
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
      meaning: 'Token 无效、已吊销或已过期',
      advice: '在上传 Token 页重新签发或轮转',
    },
    403: {
      meaning: 'Token 无上传权限，或存储配额已满',
      advice: '确认 Token 权限；配额满时删图或联系管理员扩容',
    },
    413: {
      meaning: '文件超过单图大小上限',
      advice: '压缩后重试，或调大 max_upload_mb 配置',
    },
    415: {
      meaning: '不支持的文件类型',
      advice: '仅支持 jpeg / png / gif / webp，按内容识别、不认扩展名',
    },
    422: {
      meaning: '图片分辨率超出像素预算',
      advice: '缩小分辨率后重试',
    },
    429: {
      meaning: '上传过于频繁或并发已满',
      advice: '按响应头 Retry-After 稍后重试',
    },
    503: {
      meaning: '存储渠道暂不可用',
      advice: '稍后重试；持续出现时到渠道页检查健康状态',
    },
  },
  webUploadIntro:
    '不想配客户端？在图片页点「上传图片」，走当前登录身份、无需 Token；上传后每张图都有短链，可一键复制。',
}
