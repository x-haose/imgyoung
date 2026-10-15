package channel

// driveTemplates 网盘系模板:通用 WebDAV 与需 OAuth 授权的各家网盘。
// @Description: 网盘普遍不提供稳定公开直链,故均不设公开访问地址字段、恒走代理。
var driveTemplates = []Template{
	{
		ID: "webdav", Name: "WebDAV", Description: "坚果云、Nextcloud、群晖、Alist 等",
		Category: CategoryDrive, Driver: "webdav",
		Fields: []TemplateField{
			{Key: "url", Label: "服务地址", Type: FieldText, Required: true, configKey: "url",
				Placeholder: "https://dav.jianguoyun.com/dav/",
				Help: "WebDAV 根地址。坚果云为 https://dav.jianguoyun.com/dav/;" +
					"Nextcloud/ownCloud 为 https://域名/remote.php/dav/files/用户名/;" +
					"Alist 为 https://域名/dav/;群晖为 https://域名:5006/"},
			{Key: "vendor", Label: "服务类型", Type: FieldSelect, Required: true, configKey: "vendor",
				Options: webdavVendors,
				Help:    "选错会导致列目录或分片上传行为异常;拿不准选「其他」"},
			{Key: "user", Label: "用户名", Type: FieldText, Required: true, configKey: "user"},
			{Key: "pass", Label: "密码", Type: FieldPassword, Required: true, configKey: "pass",
				Help: "坚果云须用「应用密码」而非登录密码,在其安全选项中生成"},
			drivePathField(),
		},
	},
	{
		ID: "onedrive", Name: "OneDrive", Description: "微软 OneDrive 与 SharePoint",
		Category: CategoryDrive, Driver: "onedrive", RequiresOAuth: true,
		Fields: oauthFields(TemplateField{Key: "region", Label: "区域", Type: FieldSelect, Required: true,
			configKey: "region", Options: onedriveRegions, Help: "决定授权与接口端点,须与账号所属区域一致"}),
	},
	{
		ID: "googledrive", Name: "Google Drive", Description: "谷歌云端硬盘",
		Category: CategoryDrive, Driver: "drive", RequiresOAuth: true,
		Fields: oauthFields(),
	},
	{
		ID: "dropbox", Name: "Dropbox", Description: "Dropbox 网盘",
		Category: CategoryDrive, Driver: "dropbox", RequiresOAuth: true,
		Fields: oauthFields(),
	},
	{
		ID: "pcloud", Name: "pCloud", Description: "pCloud 网盘",
		Category: CategoryDrive, Driver: "pcloud", RequiresOAuth: true,
		Fields: oauthFields(TemplateField{Key: "hostname", Label: "数据区域", Type: FieldSelect, Required: true,
			configKey: "hostname", Options: pcloudHosts, Help: "须与注册账号时所选区域一致,否则换取令牌失败"}),
	},
	{
		ID: "box", Name: "Box", Description: "Box 企业网盘",
		Category: CategoryDrive, Driver: "box", RequiresOAuth: true,
		Fields: oauthFields(),
	},
	{
		ID: "yandex", Name: "Yandex Disk", Description: "Yandex 网盘",
		Category: CategoryDrive, Driver: "yandex", RequiresOAuth: true,
		Fields: oauthFields(),
	},
}

// webdavVendors WebDAV 服务类型选项,取值须与 rclone webdav 后端一致。
var webdavVendors = []FieldOption{
	{Label: "Nextcloud", Value: "nextcloud"},
	{Label: "ownCloud 10", Value: "owncloud"},
	{Label: "ownCloud Infinite Scale", Value: "infinitescale"},
	{Label: "SharePoint Online", Value: "sharepoint"},
	{Label: "SharePoint(NTLM 自建)", Value: "sharepoint-ntlm"},
	{Label: "Fastmail Files", Value: "fastmail"},
	{Label: "rclone serve webdav", Value: "rclone"},
	{Label: "其他(坚果云 / Alist / 群晖等)", Value: "other"},
}

// onedriveRegions OneDrive 区域选项,取值须与 rclone onedrive 后端一致。
var onedriveRegions = []FieldOption{
	{Label: "国际版", Value: "global"},
	{Label: "美国政府云", Value: "us"},
	{Label: "德国云", Value: "de"},
	{Label: "中国世纪互联", Value: "cn"},
}

// pcloudHosts pCloud 数据区域选项,取值须与 rclone pcloud 后端一致。
var pcloudHosts = []FieldOption{
	{Label: "美国", Value: "api.pcloud.com"},
	{Label: "欧洲", Value: "eapi.pcloud.com"},
}

// oauthFields 需授权模板的字段:后端专属字段在前,随后是自建应用凭证、授权令牌与存储目录。
// @Description: 应用凭证随渠道保存:令牌续期必须用签发它的同一应用。
// @param lead 排在最前的后端专属字段(区域等)
// @return 字段列表
func oauthFields(lead ...TemplateField) []TemplateField {
	return append(lead, clientIDField(), clientSecretField(), oauthTokenField(), drivePathField())
}

// clientIDField 自建 OAuth 应用的 Client ID 字段。
func clientIDField() TemplateField {
	return TemplateField{Key: ConfigKeyClientID, Label: "应用 Client ID", Type: FieldText,
		configKey: ConfigKeyClientID,
		Help: "一键授权必填,填在服务商开放平台自建的应用标识;粘贴令牌时,若授权用的是自建应用也须填写。" +
			"令牌续期用它,须与签发令牌的应用一致"}
}

// clientSecretField 自建 OAuth 应用的 Client Secret 字段(密文)。
func clientSecretField() TemplateField {
	return TemplateField{Key: ConfigKeyClientSecret, Label: "应用 Client Secret", Type: FieldPassword,
		configKey: ConfigKeyClientSecret, Help: "与 Client ID 配套的应用密钥"}
}

// oauthTokenField 授权令牌字段;由授权流写入,不手工录入。
func oauthTokenField() TemplateField {
	return TemplateField{Key: ConfigKeyToken, Label: "授权", Type: FieldOAuth,
		Required: true, configKey: ConfigKeyToken,
		Help: "完成授权后自动填入,无需手工录入"}
}

// drivePathField 网盘内的存储目录字段。
func drivePathField() TemplateField {
	return TemplateField{Key: "path", Label: "存储目录", Type: FieldText, configKey: "path",
		Placeholder: "imgyoung", Help: "网盘内用于存放图片的目录,留空存放于根目录"}
}
