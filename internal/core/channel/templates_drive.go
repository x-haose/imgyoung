package channel

// driveTemplates 网盘系模板:通用 WebDAV 与需 OAuth 授权的各家网盘。
// @Description: 网盘普遍不提供稳定公开直链,故均不设公开访问地址字段、恒走代理。
var driveTemplates = []Template{
	{
		ID: "webdav", Category: CategoryDrive, Driver: "webdav",
		Fields: []TemplateField{
			{Key: "url", Type: FieldText, Required: true, configKey: "url"},
			{Key: "vendor", Type: FieldSelect, Required: true, configKey: "vendor", Options: webdavVendors},
			{Key: "user", Type: FieldText, Required: true, configKey: "user"},
			{Key: "pass", Type: FieldPassword, Required: true, configKey: "pass"},
			drivePathField(),
		},
	},
	{
		ID: "onedrive", Category: CategoryDrive, Driver: "onedrive", RequiresOAuth: true,
		Fields: oauthFields(TemplateField{Key: "region", Type: FieldSelect, Required: true,
			configKey: "region", Options: onedriveRegions}),
	},
	{
		ID: "googledrive", Category: CategoryDrive, Driver: "drive", RequiresOAuth: true,
		Fields: oauthFields(),
	},
	{
		ID: "dropbox", Category: CategoryDrive, Driver: "dropbox", RequiresOAuth: true,
		Fields: oauthFields(),
	},
	{
		ID: "pcloud", Category: CategoryDrive, Driver: "pcloud", RequiresOAuth: true,
		Fields: oauthFields(TemplateField{Key: "hostname", Type: FieldSelect, Required: true,
			configKey: "hostname", Options: pcloudHosts}),
	},
	{
		ID: "box", Category: CategoryDrive, Driver: "box", RequiresOAuth: true,
		Fields: oauthFields(),
	},
	{
		ID: "yandex", Category: CategoryDrive, Driver: "yandex", RequiresOAuth: true,
		Fields: oauthFields(),
	},
}

// webdavVendors WebDAV 服务类型的可选值,须与 rclone webdav 后端的 vendor 取值一致。
var webdavVendors = []string{
	"nextcloud", "owncloud", "infinitescale", "sharepoint", "sharepoint-ntlm", "fastmail", "rclone", "other",
}

// onedriveRegions OneDrive 区域的可选值,须与 rclone onedrive 后端的 region 取值一致。
var onedriveRegions = []string{"global", "us", "de", "cn"}

// pcloudHosts pCloud 数据区域的可选值,须与 rclone pcloud 后端的 hostname 取值一致。
var pcloudHosts = []string{"api.pcloud.com", "eapi.pcloud.com"}

// oauthFields 需授权模板的字段:后端专属字段在前,随后是自建应用凭证、授权令牌与存储目录。
// @Description: 应用凭证随渠道保存:令牌续期必须用签发它的同一应用。
// @param lead 排在最前的后端专属字段(区域等)
// @return 字段列表
func oauthFields(lead ...TemplateField) []TemplateField {
	return append(lead, clientIDField(), clientSecretField(), oauthTokenField(), drivePathField())
}

// clientIDField 自建 OAuth 应用的 Client ID 字段;一键授权必填,令牌续期也用它。
func clientIDField() TemplateField {
	return TemplateField{Key: ConfigKeyClientID, Type: FieldText, configKey: ConfigKeyClientID}
}

// clientSecretField 自建 OAuth 应用的 Client Secret 字段(密文)。
func clientSecretField() TemplateField {
	return TemplateField{Key: ConfigKeyClientSecret, Type: FieldPassword, configKey: ConfigKeyClientSecret}
}

// oauthTokenField 授权令牌字段;由授权流写入或粘贴 rclone authorize 的输出。
func oauthTokenField() TemplateField {
	return TemplateField{Key: ConfigKeyToken, Type: FieldOAuth, Required: true, configKey: ConfigKeyToken}
}

// drivePathField 网盘内的存储目录字段;留空存放于根目录。
func drivePathField() TemplateField {
	return TemplateField{Key: "path", Type: FieldText, configKey: "path"}
}
