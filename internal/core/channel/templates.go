package channel

import (
	"fmt"
	"maps"
	"strings"
)

// 字段输入类型,前端据此渲染控件。
const (
	FieldText     = "text"     // 单行文本
	FieldPassword = "password" // 密文,输入框掩码、列表不回显
	FieldSelect   = "select"   // 下拉,取 Options 之一
)

// FieldOption select 字段的一个选项。
type FieldOption struct {
	Label string `json:"label"` // 展示名(如 "美国东部(弗吉尼亚北部) us-east-1")
	Value string `json:"value"` // 实际值(如 "us-east-1")
}

// TemplateField 模板的一个用户输入字段。
type TemplateField struct {
	Key         string        `json:"key"`                   // 表单字段标识
	Label       string        `json:"label"`                 // 中文标签
	Type        string        `json:"type"`                  // 输入类型,见 Field* 常量
	Required    bool          `json:"required"`              // 是否必填
	Placeholder string        `json:"placeholder,omitempty"` // 占位示例
	Help        string        `json:"help,omitempty"`        // 帮助说明
	Options     []FieldOption `json:"options,omitempty"`     // select 选项
	configKey   string        // 值写入渠道 config 的键;空表示仅参与 endpoint 拼接、不直接落配置
}

// Template 一个渠道模板:预填 rclone 后端与固定参数,只暴露必要字段给用户填。
type Template struct {
	ID          string          `json:"id"`          // 模板标识
	Name        string          `json:"name"`        // 展示名
	Description string          `json:"description"` // 一句话说明
	Driver      string          `json:"driver"`      // rclone 后端名
	Fields      []TemplateField `json:"fields"`      // 用户输入字段
	// 以下为后端合并逻辑所需,不下发前端。
	preset          map[string]string // 对用户隐藏的固定参数(provider 等)
	endpointPattern string            // endpoint 拼接模板,{fieldKey} 占位;空表示无需拼接
}

// ErrUnknownTemplate 指定的模板不存在。
var ErrUnknownTemplate = fmt.Errorf("%w: 未知的渠道模板", ErrInvalidChannel)

// templates 内置渠道模板清单,当前覆盖 S3 系对象存储 7 类。
// 底层都是 rclone s3 后端,差异仅在 provider / endpoint / region,预设值经 workflow
// 查证 rclone 官方 s3 文档校准。凭据统一 access_key_id / secret_access_key。
var templates = []Template{
	{
		ID: "aws-s3", Name: "Amazon S3", Description: "AWS 官方对象存储",
		Driver: "s3", preset: map[string]string{"provider": "AWS"},
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "region", Label: "区域", Type: FieldSelect, Required: true, configKey: "region", Options: awsRegions},
			bucketField(""), publicBaseURLField(),
		},
	},
	{
		ID: "r2", Name: "Cloudflare R2", Description: "Cloudflare 对象存储,零出口流量费",
		Driver: "s3", preset: map[string]string{"provider": "Cloudflare", "region": "auto"},
		endpointPattern: "https://{account_id}.r2.cloudflarestorage.com",
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "account_id", Label: "账户 ID", Type: FieldText, Required: true,
				Placeholder: "如 a1b2c3d4e5f6", Help: "Cloudflare 账户 ID,用于拼接 R2 端点"},
			bucketField(""), publicBaseURLField(),
		},
	},
	{
		ID: "cos", Name: "腾讯云 COS", Description: "腾讯云对象存储",
		Driver: "s3", preset: map[string]string{"provider": "TencentCOS"},
		endpointPattern: "cos.{region}.myqcloud.com",
		Fields: []TemplateField{
			{Key: "access_key_id", Label: "SecretId", Type: FieldText, Required: true, configKey: "access_key_id"},
			{Key: "secret_access_key", Label: "SecretKey", Type: FieldPassword, Required: true, configKey: "secret_access_key"},
			{Key: "region", Label: "区域", Type: FieldSelect, Required: true, Options: cosRegions},
			bucketField("桶名需带 APPID 后缀,如 mybucket-1250000000"), publicBaseURLField(),
		},
	},
	{
		ID: "oss", Name: "阿里云 OSS", Description: "阿里云对象存储",
		Driver: "s3", preset: map[string]string{"provider": "Alibaba"},
		endpointPattern: "{region}.aliyuncs.com",
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "region", Label: "区域", Type: FieldSelect, Required: true, Options: ossRegions},
			bucketField(""), publicBaseURLField(),
		},
	},
	{
		ID: "b2", Name: "Backblaze B2", Description: "Backblaze B2(S3 兼容接口)",
		Driver: "s3", preset: map[string]string{"provider": "Other"},
		endpointPattern: "https://s3.{region}.backblazeb2.com",
		Fields: []TemplateField{
			{Key: "access_key_id", Label: "Key ID", Type: FieldText, Required: true, configKey: "access_key_id"},
			{Key: "secret_access_key", Label: "Application Key", Type: FieldPassword, Required: true, configKey: "secret_access_key"},
			{Key: "region", Label: "区域", Type: FieldText, Required: true, configKey: "region",
				Placeholder: "如 us-west-004", Help: "B2 账户所在区域,须与端点一致"},
			bucketField(""), publicBaseURLField(),
		},
	},
	{
		ID: "minio", Name: "MinIO / 自建", Description: "MinIO 或其他自建 S3 服务",
		Driver: "s3", preset: map[string]string{"provider": "Minio"},
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "endpoint", Label: "服务端点", Type: FieldText, Required: true, configKey: "endpoint",
				Placeholder: "https://minio.example.com"},
			bucketField(""), publicBaseURLField(),
		},
	},
	{
		ID: "s3-compatible", Name: "S3 兼容(通用)", Description: "任意兼容 S3 协议的服务",
		Driver: "s3", preset: map[string]string{"provider": "Other"},
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "endpoint", Label: "S3 API 端点", Type: FieldText, Required: true, configKey: "endpoint",
				Placeholder: "https://s3.example.com"},
			bucketField(""), publicBaseURLField(),
		},
	},
}

// accessKeyField 通用 Access Key 字段。
func accessKeyField() TemplateField {
	return TemplateField{Key: "access_key_id", Label: "Access Key", Type: FieldText, Required: true, configKey: "access_key_id"}
}

// secretKeyField 通用 Secret Key 字段(密文)。
func secretKeyField() TemplateField {
	return TemplateField{Key: "secret_access_key", Label: "Secret Key", Type: FieldPassword, Required: true, configKey: "secret_access_key"}
}

// bucketField 通用 Bucket 字段;值落入渠道 path(渠道内根路径),extraHelp 追加提示。
func bucketField(extraHelp string) TemplateField {
	help := "存储桶名称,可加 / 前缀限定子目录"
	if extraHelp != "" {
		help = extraHelp
	}
	return TemplateField{Key: "bucket", Label: "存储桶", Type: FieldText, Required: true, configKey: "path", Help: help}
}

// publicBaseURLField 通用公开访问地址字段(可选)。
func publicBaseURLField() TemplateField {
	return TemplateField{Key: "public_base_url", Label: "公开访问地址", Type: FieldText,
		Placeholder: "https://cdn.example.com", configKey: "public_base_url",
		Help: "可选;存储桶(或其 CDN)的公开访问 URL 前缀,配置后该渠道可 302 直链"}
}

// Templates 返回内置模板清单(供 API 下发)。
// @return 模板切片
func Templates() []Template { return templates }

// BuildTemplateConfig 按模板与用户输入合并出渠道 config,并做必填校验。
// @Description: 合并顺序为固定预设 → 字段值(按 configKey 落位)→ endpoint 拼接;
// 参与拼接的中间字段(region/account_id)若 configKey 为空则不直接进 config。
// @param templateID 模板标识
// @param inputs 用户填写的字段值(键为 TemplateField.Key)
// @return rclone 后端名
// @return 合并后的渠道 config(含约定键 path)
// @return ErrUnknownTemplate 或字段必填校验失败的 ErrInvalidChannel
func BuildTemplateConfig(templateID string, inputs map[string]string) (string, map[string]string, error) {
	tpl := templateByID(templateID)
	if tpl == nil {
		return "", nil, ErrUnknownTemplate
	}
	config := maps.Clone(tpl.preset)
	if config == nil {
		config = map[string]string{}
	}
	for _, f := range tpl.Fields {
		v := strings.TrimSpace(inputs[f.Key])
		if f.Required && v == "" {
			return "", nil, fmt.Errorf("%w: 字段「%s」必填", ErrInvalidChannel, f.Label)
		}
		if f.configKey != "" && v != "" {
			config[f.configKey] = v
		}
	}
	if tpl.endpointPattern != "" {
		ep := tpl.endpointPattern
		for _, f := range tpl.Fields {
			ep = strings.ReplaceAll(ep, "{"+f.Key+"}", strings.TrimSpace(inputs[f.Key]))
		}
		config["endpoint"] = ep
	}
	return tpl.Driver, config, nil
}

// templateByID 按标识查内置模板。
// @param templateID 模板标识
// @return 命中的模板;不存在返回 nil
func templateByID(templateID string) *Template {
	for i := range templates {
		if templates[i].ID == templateID {
			return &templates[i]
		}
	}
	return nil
}

// TemplateInputsFromConfig 从渠道 config 反解出模板表单字段值(供编辑表单预填)。
// @Description: 与 BuildTemplateConfig 互逆——直落 configKey 的字段取同键值,
// 参与 endpoint 拼接的字段按模板 endpoint 模式反向提取;密码字段以
// SecretPlaceholder 掩码,凭证明文不下发。
// @param templateID 模板标识
// @param config 解密后的渠道 config
// @return 表单字段值(键为 TemplateField.Key)
// @return 模板是否存在
func TemplateInputsFromConfig(templateID string, config map[string]string) (map[string]string, bool) {
	tpl := templateByID(templateID)
	if tpl == nil {
		return nil, false
	}
	inputs := make(map[string]string, len(tpl.Fields))
	for _, f := range tpl.Fields {
		switch {
		case f.Type == FieldPassword:
			if config[f.configKey] != "" {
				inputs[f.Key] = SecretPlaceholder
			}
		case f.configKey != "":
			if v := config[f.configKey]; v != "" {
				inputs[f.Key] = v
			}
		case tpl.endpointPattern != "":
			if v, ok := extractPatternField(tpl.endpointPattern, f.Key, config["endpoint"]); ok {
				inputs[f.Key] = v
			}
		}
	}
	return inputs, true
}

// extractPatternField 从按模式拼接出的 endpoint 中反向提取一个占位字段的值。
// @Description: 以占位符两侧的字面前后缀切出中间值;仅支持模式含单个占位符的
// 形态(内置模板均如此),前后缀含其他占位符时提取失败而非误配。
// @param pattern endpoint 拼接模式,{fieldKey} 占位
// @param key 待提取的字段标识
// @param endpoint 实际 endpoint 值
// @return 提取出的字段值
// @return 是否提取成功
func extractPatternField(pattern, key, endpoint string) (string, bool) {
	marker := "{" + key + "}"
	i := strings.Index(pattern, marker)
	if i < 0 || endpoint == "" {
		return "", false
	}
	prefix, suffix := pattern[:i], pattern[i+len(marker):]
	if !strings.HasPrefix(endpoint, prefix) || !strings.HasSuffix(endpoint, suffix) ||
		len(endpoint) < len(prefix)+len(suffix) {
		return "", false
	}
	v := endpoint[len(prefix) : len(endpoint)-len(suffix)]
	return v, v != ""
}
