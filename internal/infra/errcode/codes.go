package errcode

// 通用。
var (
	Internal           = define("internal", "服务内部错误")
	InvalidBody        = define("invalid_body", "请求体不是合法 JSON,或含有未知字段")
	Unauthenticated    = define("unauthenticated", "未登录或会话已过期")
	CSRFRejected       = define("csrf_rejected", "CSRF 校验失败")
	AdminRequired      = define("admin_required", "需要管理员权限")
	MustChangePassword = define("must_change_password", "须先修改出厂默认密码")
	SourceNotAllowed   = define("source_not_allowed", "来源地址不在放行网段内")
	NoHealthyChannel   = define("no_healthy_channel", "没有健康的渠道可用")
	LangInvalid        = define("lang_invalid", "lang 取值非法(可选 zh-CN / en)")
)

// 登录与账号。
var (
	LoginRateLimited     = define("login_rate_limited", "登录尝试过于频繁,请稍后再试")
	InvalidCredentials   = define("invalid_credentials", "用户名或密码错误")
	UserDisabled         = define("user_disabled", "该用户已被禁用")
	OldPasswordIncorrect = define("old_password_incorrect", "旧密码不正确")
	PasswordTooShort     = define("password_too_short", "密码至少 {min} 位")
	UserNameTaken        = define("user_name_taken", "登录名已存在")
	InvalidRole          = define("invalid_role", "角色取值非法")
	UserNotFound         = define("user_not_found", "用户不存在")
	UserProtected        = define("user_protected", "内置管理员不可禁用、降级或删除")
	CannotDeleteSelf     = define("cannot_delete_self", "不能删除当前登录用户")
	UserHasImages        = define("user_has_images", "用户名下仍有图片,请先删除或处置")
	UserTTLInvalid       = define("user_ttl_invalid", "default_ttl_hours 需为 0(永久)到 {max} 的整数,或 null 表示随全局")
)

// 上传 Token。
var (
	TokenRequestInvalid = define("token_request_invalid", "请求体非法:名称必填且不超过 {max_name} 字节,有效期小时数需在 0 到 {max_hours} 之间")
	TokenNameTaken      = define("token_name_taken", "同名 Token 已存在")
	TokenIDInvalid      = define("token_id_invalid", "Token ID 非法")
	TokenNotFound       = define("token_not_found", "Token 不存在或已失效")
	TokenActive         = define("token_active", "Token 仍有效,请先吊销再删除")
)

// 图片管理。
var (
	ImageNotFound          = define("image_not_found", "图片不存在")
	ImagePatchInvalid      = define("image_patch_invalid", "请求体非法:ttl_hours 需为 0(永久)或不超过 {max} 的正整数,至少携带一个修改项")
	CleanupExemptAdminOnly = define("cleanup_exempt_admin_only", "清理豁免仅管理员可设置")
	BatchIDsInvalid        = define("batch_ids_invalid", "请求体非法:ids 需为 1 到 {max} 个图片 ID")
)

// 相似图裁决。
var (
	CandidateIDInvalid = define("candidate_id_invalid", "候选 ID 非法")
	CandidateNotFound  = define("candidate_not_found", "候选不存在或已裁决")
	KeeperInvalid      = define("keeper_invalid", "保留对象必须是候选对中的一方")
	CandidateStale     = define("candidate_stale", "候选涉及的图片已被删除,请刷新列表")
)

// 防盗链白名单。
var (
	RefererPatternInvalid = define("referer_pattern_invalid", "规则格式非法,应为裸域(example.com)或子域通配(*.example.com)")
	RefererExists         = define("referer_exists", "该规则已存在")
	RefererIDInvalid      = define("referer_id_invalid", "规则 id 非法")
	RefererNotFound       = define("referer_not_found", "规则不存在")
)

// 元数据库快照。
var (
	BackupSnapshotFailed = define("backup_snapshot_failed", "生成数据库快照失败: {detail}")
	BackupStoreFailed    = define("backup_store_failed", "快照 {snapshot} 有 {failed}/{total} 个渠道未写成({detail})")
	SnapshotNameInvalid  = define("snapshot_name_invalid", "快照名非法")
	SnapshotNotFound     = define("snapshot_not_found", "快照不存在")
)

// 运营配置。
var (
	SettingsBodyInvalid = define("settings_body_invalid", "请求体需为非空的 键→值 JSON 对象")
	SettingUnknown      = define("setting_unknown", "未知配置键 {key}")
	SettingType         = define("setting_type", "{key} 的取值类型不对")
	SettingPositive     = define("setting_positive", "{key}={value} 非法: 需要正整数")
	SettingNonNegative  = define("setting_non_negative", "{key}={value} 非法: 不可为负")
	SettingTooLarge     = define("setting_too_large", "{key}={value} 非法: 不可超过 {max}")
	SettingRange        = define("setting_range", "{key}={value} 非法: 需要 {min} 到 {max} 之间的整数")
	SettingOption       = define("setting_option", "{key}={value} 非法: 可选 {options}")
	SettingURL          = define("setting_url", "{key}={value} 非法: 需要 http/https 地址")
	SettingSiteRoot     = define("setting_site_root", "{key}={value} 非法: 需要站点根地址,形如 https://img.example.com(不带路径与查询参数)")
	SettingEmpty        = define("setting_empty", "{key} 不可为空")
)

// 渠道管理。
var (
	ChannelIDInvalid          = define("channel_id_invalid", "渠道 ID 非法")
	ChannelNotFound           = define("channel_not_found", "渠道不存在")
	ChannelNameExists         = define("channel_name_exists", "渠道名已存在")
	ChannelDeleting           = define("channel_deleting", "渠道正在清理中,不能修改或探测")
	ChannelEncryptedImmutable = define("channel_encrypted_immutable", "加密属性创建后不可变(变更会造成密文/明文混存且不可恢复);如需调整请新建渠道并迁移数据")
	ChannelSoleReplicas       = define("channel_sole_replicas", "该渠道持有 {count} 张图片的唯一副本,删除会丢数据;请先迁移或删除相关图片")
	PurgeParamInvalid         = define("purge_param_invalid", "purge 参数须为 true 或 false")
	ChannelNameDriverRequired = define("channel_name_driver_required", "渠道参数非法: name 与 driver 必填")
	ChannelNameTooLong        = define("channel_name_too_long", "渠道参数非法: name 超过 {max} 字节")
	ChannelEncryptedDirect    = define("channel_encrypted_direct_link", "渠道参数非法: 加密渠道不允许直链(密文无法直出)")
	ChannelConfigKeyInvalid   = define("channel_config_key_invalid", "渠道参数非法: 配置键 {key} 非法(仅允许字母数字下划线)")
	ChannelKeepNotSecret      = define("channel_keep_not_secret", "渠道参数非法: 字段 {field} 不是凭证,不能沿用已存值")
	ChannelKeepNoValue        = define("channel_keep_no_value", "渠道参数非法: 字段 {field} 无已存原值可保留,请重新填写")
	ChannelUnknownBackend     = define("channel_unknown_backend", "渠道参数非法: 未知的后端 {driver}")
	ChannelUnknownTemplate    = define("channel_unknown_template", "渠道参数非法: 未知的渠道模板")
	ChannelFieldRequired      = define("channel_field_required", "渠道参数非法: 字段 {field} 必填")
	ChannelParamInvalid       = define("channel_param_invalid", "渠道参数非法: {detail}")
	ChannelBackendFailed      = define("channel_backend_failed", "渠道参数非法: 后端初始化失败: {detail}")
	ChannelBucketFailed       = define("channel_bucket_failed", "自动创建存储桶失败(请确认密钥具备建桶权限,或先在控制台建好桶并关闭此项): {detail}")
	ChannelUnrepresentable    = define("channel_unrepresentable", "参数值无法写进 rclone 配置文件: 远端 {remote} 的参数 {key}")
)

// 网盘授权。
var (
	OAuthTokenInvalid    = define("oauth_token_invalid", "渠道参数非法: 授权令牌格式非法")
	OAuthNoRefreshToken  = define("oauth_no_refresh_token", "渠道参数非法: 授权令牌缺少刷新令牌,访问令牌过期后无法续期")
	OAuthNotRequired     = define("oauth_not_required", "渠道参数非法: 该后端无需 OAuth 授权")
	OAuthOptionRequired  = define("oauth_option_required", "渠道参数非法: 后端 {driver} 要求先提供 {option}")
	OAuthBaseURLRequired = define("oauth_base_url_required", "一键授权需先在设置中填写对外站点地址;或改用粘贴令牌方式")
	OAuthSessionNotFound = define("oauth_session_not_found", "授权会话不存在或已过期")
	OAuthDenied          = define("oauth_denied", "授权被拒绝或取消: {detail}")
	OAuthExchangeFailed  = define("oauth_exchange_failed", "换取令牌失败,请核对应用凭证与回调地址")
)

// 上传。
var (
	UploadTokenInvalid    = define("upload_token_invalid", "无效的上传 Token")
	UploadTokenScope      = define("upload_token_scope", "Token 无本操作权限")
	UploadUnauthenticated = define("upload_unauthenticated", "未认证的上传请求")
	UploadRateLimited     = define("upload_rate_limited", "上传过于频繁,请稍后重试")
	UploadBusy            = define("upload_busy", "上传并发已满,请稍后重试")
	UploadUnsupportedType = define("upload_unsupported_type", "仅支持图片文件(jpeg/png/gif/webp)")
	UploadTooManyPixels   = define("upload_too_many_pixels", "图片分辨率超出限制(像素预算 {max} 百万像素)")
	UploadInvalidImage    = define("upload_invalid_image", "图片内容无法解析")
	UploadEXIFPolicy      = define("upload_exif_policy", "exif_policy 取值非法(可选 strip / passthrough)")
	UploadQuotaExceeded   = define("upload_quota_exceeded", "存储配额已满")
	UploadUnavailable     = define("upload_unavailable", "存储渠道暂不可用,请稍后重试")
	UploadTTLInvalid      = define("upload_ttl_invalid", "ttl 取值非法(需要 0 到 {max} 的整数秒,或 never 表示永久)")
	UploadTooLarge        = define("upload_too_large", "文件超过大小上限({max} MB)")
	UploadFieldTooLong    = define("upload_field_too_long", "exif_policy 或 ttl 字段过长(上限 {max} 字节)")
	UploadNoFile          = define("upload_no_file", "无法读取 file 字段(需要 multipart/form-data 的 file 字段)")
	URLBodyInvalid        = define("url_body_invalid", "请求体需为 JSON 对象,且只含 url/file_name/exif_policy/ttl 字段")
	URLRequired           = define("url_required", "url 不能为空")
	URLScheme             = define("url_scheme", "url 仅支持 http/https 地址")
	URLBlocked            = define("url_blocked", "目标地址不可访问(仅允许公网地址)")
	URLTooManyRedirects   = define("url_too_many_redirects", "远端重定向次数过多")
	URLTooLarge           = define("url_too_large", "远端文件超过大小上限({max} MB)")
	URLBadStatus          = define("url_bad_status", "远端返回错误状态,无法取回图片")
	URLFetchFailed        = define("url_fetch_failed", "无法从该地址取回图片")
	VariantParamsInvalid  = define("variant_params_invalid", "非法的处理参数:宽度或格式不在允许档位")
)
