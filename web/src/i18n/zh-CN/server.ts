import type { components } from '@/lib/api.gen'

type Schemas = components['schemas']

/** 服务端返回的错误、失败原因与告警的文案;键与接口契约的枚举逐一对应,缺一项即类型检查失败。 */
export const server = {
  /** 错误信封的文案,按错误码;{name} 占位符取自信封的 params。 */
  errors: {
    internal: '服务内部错误',
    invalid_body: '请求体不是合法 JSON，或含有未知字段',
    unauthenticated: '未登录或会话已过期',
    csrf_rejected: 'CSRF 校验失败',
    admin_required: '需要管理员权限',
    must_change_password: '须先修改初始密码',
    source_not_allowed: '来源地址不在放行网段内',
    no_healthy_channel: '没有健康的渠道可用',
    lang_invalid: 'lang 取值非法（可选 zh-CN / en）',
    query_param_invalid: '查询参数 {name} 取值非法',
    login_rate_limited: '登录尝试过于频繁，请稍后再试',
    invalid_credentials: '用户名或密码错误',
    user_disabled: '该用户已被禁用',
    old_password_incorrect: '旧密码不正确',
    password_too_short: '密码至少 {min} 位',
    user_name_taken: '登录名已存在',
    invalid_role: '角色取值非法',
    user_not_found: '用户不存在',
    user_protected: '内置管理员不可禁用、降级或删除',
    cannot_delete_self: '不能删除当前登录用户',
    user_has_images: '该用户名下仍有图片，请先删除这些图片',
    user_ttl_invalid:
      'default_ttl_hours 须为 0（永久）到 {max} 的整数，或为 null（跟随全局默认）',
    token_request_invalid:
      '请求体非法：名称必填且不超过 {max_name} 个字符，有效期小时数需在 0 到 {max_hours} 之间',
    token_name_taken: '同名 Token 已存在',
    token_id_invalid: 'Token ID 非法',
    token_not_found: 'Token 不存在或已失效',
    token_active: 'Token 仍有效，请先吊销再删除',
    image_not_found: '图片不存在',
    image_patch_invalid:
      '请求体非法：ttl_hours 须为 0（永久）或不超过 {max} 的正整数，且至少包含一项修改',
    cleanup_exempt_admin_only: '清理豁免仅管理员可设置',
    image_expired: '图片已到期，不能再修改有效期',
    batch_ids_invalid: '请求体非法：ids 需为 1 到 {max} 个图片 ID',
    candidate_id_invalid: '候选 ID 非法',
    candidate_not_found: '候选不存在或已裁决',
    keeper_invalid: '保留的图片必须是这对候选中的一张',
    candidate_stale: '候选涉及的图片已被删除，请刷新列表',
    referer_pattern_invalid:
      '规则格式非法，应为裸域（example.com）或子域通配（*.example.com）',
    referer_exists: '该规则已存在',
    referer_id_invalid: '规则 id 非法',
    referer_not_found: '规则不存在',
    backup_snapshot_failed: '生成数据库快照失败：{detail}',
    backup_store_failed:
      '快照 {snapshot} 有 {failed}/{total} 个渠道写入失败（{detail}）',
    backup_prune_failed:
      '快照 {snapshot} 已写入，但有 {failed}/{total} 个渠道清理旧快照失败，保留份数未生效（{detail}）',
    recovery_busy: '已有一轮找回在进行，请等它结束后再试',
    import_input_invalid: '导入请求不合法：{detail}',
    import_not_found: '导入任务不存在',
    import_state_invalid: '任务当前状态不允许此操作',
    import_no_selection: '所选域名下没有待导入的图片地址',
    import_nothing_to_retry: '没有需要重试的条目',
    import_owner_only: '只有创建这个导入任务的用户能执行此操作',
    import_listing_invalid: '站点地图或订阅源的内容无法解析',
    snapshot_name_invalid: '快照名非法',
    snapshot_not_found: '快照不存在',
    snapshot_corrupt:
      '快照 {snapshot} 不是完整的数据库文件（可能写入途中被中断），请换一份快照恢复',
    settings_body_invalid: '请求体需为非空的 键→值 JSON 对象',
    setting_unknown: '未知配置键 {key}',
    setting_type: '{key} 的取值类型错误',
    setting_positive: '{key}={value} 非法：需要正整数',
    setting_non_negative: '{key}={value} 非法：不可为负',
    setting_too_large: '{key}={value} 非法：不可超过 {max}',
    setting_range: '{key}={value} 非法：需要 {min} 到 {max} 之间的整数',
    setting_option: '{key}={value} 非法：可选 {options}',
    setting_url: '{key}={value} 非法：需要 http/https 地址',
    setting_site_root:
      '{key}={value} 非法：需要站点根地址，形如 https://img.example.com（不带路径与查询参数）',
    setting_empty: '{key} 不可为空',
    channel_id_invalid: '渠道 ID 非法',
    channel_not_found: '渠道不存在',
    channel_name_exists: '渠道名已存在',
    channel_deleting: '渠道正在清理中，不能修改或探测',
    channel_config_changed:
      '渠道配置在保存期间已被改动（另一次编辑，或服务商刷新了授权令牌），请重新保存',
    channel_encrypted_immutable:
      '渠道创建后不能更改加密设置；如需更改，请新建渠道并迁移数据',
    channel_sole_replicas:
      '该渠道上有 {count} 张图片的唯一副本，删除会丢失数据；请先迁移或删除这些图片',
    purge_param_invalid: 'purge 参数须为 true 或 false',
    channel_name_driver_required: '渠道参数非法：name 与 driver 必填',
    channel_name_too_long: '渠道参数非法：name 超过 {max} 字节',
    channel_encrypted_direct_link: '渠道参数非法：加密渠道不能开启 302 直链',
    channel_local_path_relative:
      '渠道参数非法：local 渠道的路径须为绝对路径，收到 {path}',
    channel_config_key_invalid:
      '渠道参数非法：配置键 {key} 非法（只能包含字母、数字和下划线）',
    channel_keep_not_secret:
      '渠道参数非法：字段 {field} 不是凭证，不能保留原值',
    channel_keep_no_value:
      '渠道参数非法：字段 {field} 没有可保留的原值，请重新填写',
    channel_unknown_backend: '渠道参数非法：未知的后端 {driver}',
    channel_unknown_template: '渠道参数非法：未知的渠道模板',
    channel_field_required: '渠道参数非法：字段 {field} 必填',
    channel_param_invalid: '渠道参数非法：{detail}',
    channel_backend_failed: '渠道参数非法：后端初始化失败：{detail}',
    channel_bucket_failed:
      '自动创建存储桶失败（请确认密钥有建桶权限，或在控制台建好存储桶后关闭自动创建）：{detail}',
    channel_unrepresentable:
      '参数值无法写进 rclone 配置文件：远端 {remote} 的参数 {key}',
    oauth_token_invalid: '渠道参数非法：授权令牌格式非法',
    oauth_no_refresh_token:
      '渠道参数非法：授权令牌缺少刷新令牌，访问令牌过期后无法续期',
    oauth_not_required: '渠道参数非法：该后端无需 OAuth 授权',
    oauth_option_required: '渠道参数非法：后端 {driver} 要求先提供 {option}',
    oauth_base_url_required:
      '一键授权需先在设置中填写对外站点地址，或改用粘贴令牌',
    oauth_session_not_found: '授权会话不存在或已过期',
    oauth_denied: '授权被拒绝或取消：{detail}',
    oauth_exchange_failed: '换取令牌失败，请核对应用凭证与回调地址',
    upload_token_invalid: '无效的上传 Token',
    upload_token_scope: 'Token 没有此操作的权限',
    upload_unauthenticated: '未认证的上传请求',
    upload_rate_limited: '上传过于频繁，请稍后重试',
    upload_busy: '已达上传并发上限，请稍后重试',
    upload_unsupported_type: '仅支持图片文件（jpeg/png/gif/webp）',
    upload_too_many_pixels: '图片分辨率超过像素预算（{max} 百万像素）',
    upload_invalid_image: '图片内容无法解析',
    upload_exif_policy: 'exif_policy 取值非法（可选 strip / passthrough）',
    upload_quota_exceeded: '存储配额已满',
    upload_unavailable: '存储渠道暂不可用，请稍后重试',
    upload_ttl_invalid:
      'ttl 取值非法（需要 0 到 {max} 的整数秒，或 never 表示永久）',
    upload_too_large: '文件超过大小上限（{max} MB）',
    upload_field_too_long: 'exif_policy 或 ttl 字段过长（上限 {max} 字节）',
    upload_no_file:
      '缺少 file 字段，或请求体不是合法的 multipart/form-data 表单',
    url_body_invalid:
      '请求体需为 JSON 对象，且只含 url/file_name/exif_policy/ttl 字段',
    url_required: 'url 不能为空',
    url_scheme: 'url 仅支持 http/https 地址',
    url_blocked: '目标地址不是公网地址，不允许抓取',
    url_too_many_redirects: '远端重定向次数过多',
    url_too_large: '远端文件超过大小上限（{max} MB）',
    url_bad_status: '远端返回错误状态，无法取回图片',
    url_fetch_failed: '无法从该地址取回图片',
    variant_params_invalid: '处理参数非法：w 或 format 不在允许的档位内',
  } satisfies Record<Schemas['ErrorCode'], string>,
  /** 失败归类的说明。 */
  failureReasons: {
    s3_auth: '认证失败（403）：检查 Access Key / Secret Key 及其权限',
    app_credentials:
      '应用凭证无效：检查 Client ID / Client Secret 是否与签发令牌的应用一致',
    grant_revoked: '授权已失效：刷新令牌被拒绝，请重新授权',
    auth: '认证失败（401）：检查用户名、密码或授权令牌是否有效',
    forbidden: '无访问权限（403）：检查账号对存储位置的权限',
    bucket_missing: '存储桶不存在或无权访问：检查桶名与凭证权限',
    region_mismatch: '区域不匹配：检查所选区域与存储桶实际所在区域',
    dns: '无法解析服务端点域名：检查 endpoint 配置',
    connect: '无法连接服务端点：检查 endpoint 地址与端口',
    tls: 'TLS 证书校验失败：检查端点证书或改用正确协议',
    timeout: '连接超时：检查端点地址与网络连通性',
    permission: '存储路径无写入权限：检查目录权限',
    probe_mismatch: '探测对象读回的内容与写入的不一致：存储端改写或截断了对象',
    config_unreadable: '渠道配置无法解密：当前主密钥与保存配置时用的不同',
    no_source: '没有可读的源副本',
    channel_unavailable: '目标渠道未装载',
    object_missing: '对账发现渠道上缺少该对象',
    unclassified: '未能归类的错误',
  } satisfies Record<Schemas['FailureReason'], string>,
  /** 渠道失败环节加在归类说明前的前缀;探测环节不加。 */
  failureStages: {
    probe: '',
    load: '装载失败：',
    drain:
      '清理渠道对象失败，会自动重试；凭证已失效或存储桶已不存在时可直接移除（远端对象保留）：',
  } satisfies Record<Schemas['ChannelFailure']['stage'], string>,
  /** 归类说明后接根因原文。 */
  failureWithDetail: '{reason}（{detail}）',
  /** 告警描述,按告警类型;{name} 占位符取自告警的 params。 */
  alerts: {
    channel_unhealthy: '{failure}',
    replication_failed: '{failed} 份副本补齐失败，渠道恢复健康后会重试',
    channel_quota_exceeded: '用量 {bytes} 已超过阈值 {quota}，留意账单与容量',
    drain_failed: '{failure}',
    backup_failed: '{error}',
  } satisfies Record<Schemas['Alert']['kind'], string>,
  /** 渠道尚未探测过却不健康时的告警描述。 */
  unhealthyUnprobed: '渠道不健康',
}
