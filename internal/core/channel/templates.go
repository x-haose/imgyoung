package channel

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// 字段输入类型,前端据此渲染控件。
const (
	FieldText     = "text"     // 单行文本
	FieldPassword = "password" // 密文,输入框掩码、列表不回显
	FieldSelect   = "select"   // 下拉,取 Options 之一
	FieldOAuth    = "oauth"    // 授权令牌,由授权流写入而非手工录入
)

// 渠道模板分类,前端据此分组展示。
const (
	CategoryObjectStorage = "object_storage" // 对象存储
	CategoryDrive         = "drive"          // 网盘
)

// TemplateField 模板的一个用户输入字段。
// @Description: 只描述结构;标签、占位示例与帮助说明是显示文案,由前端按模板 id 与字段 key 查词典。
type TemplateField struct {
	Key       string   `json:"key"`               // 表单字段标识,也是前端词典里字段文案的键
	Type      string   `json:"type"`              // 输入类型,见 Field* 常量
	Required  bool     `json:"required"`          // 是否必填
	Options   []string `json:"options,omitempty"` // select 字段的可选值,即写入配置的值;显示名由前端按值查词典
	configKey string   // 值写入渠道 config 的键;空表示仅参与 endpoint 拼接、不直接落配置
}

// Template 一个渠道模板:预填 rclone 后端与固定参数,只暴露必要字段给用户填。
// @Description: 模板名与一句话说明是显示文案,由前端按模板 id 查词典。
type Template struct {
	ID            string          `json:"id"`             // 模板标识,也是前端词典里模板文案的键
	Driver        string          `json:"driver"`         // rclone 后端名
	Category      string          `json:"category"`       // 分类,见 Category* 常量
	RequiresOAuth bool            `json:"requires_oauth"` // 是否须先完成授权才能保存
	Fields        []TemplateField `json:"fields"`         // 用户输入字段
	// 以下为后端合并逻辑所需,不下发前端。
	preset          map[string]string // 对用户隐藏的固定参数(provider 等)
	endpointPattern string            // endpoint 拼接模板,{fieldKey} 占位;空表示无需拼接
}

// ErrUnknownTemplate 指定的模板不存在。
var ErrUnknownTemplate = fmt.Errorf("%w: 未知的渠道模板", ErrInvalidChannel)

// templates 内置渠道模板清单。
var templates = slices.Concat(s3Templates, driveTemplates)

// s3Templates S3 系对象存储模板。
// 底层都是 rclone s3 后端,差异仅在 provider / endpoint / region。
// 凭据统一 access_key_id / secret_access_key。
var s3Templates = []Template{
	{
		ID: "aws-s3", Category: CategoryObjectStorage, Driver: "s3", preset: map[string]string{"provider": "AWS"},
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "region", Type: FieldSelect, Required: true, configKey: "region", Options: awsRegions},
			bucketField(), publicBaseURLField(),
		},
	},
	{
		ID: "r2", Category: CategoryObjectStorage, Driver: "s3",
		preset:          map[string]string{"provider": "Cloudflare", "region": "auto"},
		endpointPattern: "https://{account_id}.r2.cloudflarestorage.com",
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "account_id", Type: FieldText, Required: true},
			bucketField(), publicBaseURLField(),
		},
	},
	{
		ID: "cos", Category: CategoryObjectStorage, Driver: "s3", preset: map[string]string{"provider": "TencentCOS"},
		endpointPattern: "cos.{region}.myqcloud.com",
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "region", Type: FieldSelect, Required: true, Options: cosRegions},
			bucketField(), publicBaseURLField(),
		},
	},
	{
		ID: "oss", Category: CategoryObjectStorage, Driver: "s3", preset: map[string]string{"provider": "Alibaba"},
		endpointPattern: "{region}.aliyuncs.com",
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "region", Type: FieldSelect, Required: true, Options: ossRegions},
			bucketField(), publicBaseURLField(),
		},
	},
	{
		ID: "b2", Category: CategoryObjectStorage, Driver: "s3", preset: map[string]string{"provider": "Other"},
		endpointPattern: "https://s3.{region}.backblazeb2.com",
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "region", Type: FieldText, Required: true, configKey: "region"},
			bucketField(), publicBaseURLField(),
		},
	},
	{
		ID: "minio", Category: CategoryObjectStorage, Driver: "s3", preset: map[string]string{"provider": "Minio"},
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "endpoint", Type: FieldText, Required: true, configKey: "endpoint"},
			bucketField(), publicBaseURLField(),
		},
	},
	{
		ID: "s3-compatible", Category: CategoryObjectStorage, Driver: "s3",
		preset: map[string]string{"provider": "Other"},
		Fields: []TemplateField{
			accessKeyField(), secretKeyField(),
			{Key: "endpoint", Type: FieldText, Required: true, configKey: "endpoint"},
			bucketField(), publicBaseURLField(),
		},
	},
}

// accessKeyField 通用访问密钥 ID 字段。
func accessKeyField() TemplateField {
	return TemplateField{Key: "access_key_id", Type: FieldText, Required: true, configKey: "access_key_id"}
}

// secretKeyField 通用访问密钥字段(密文)。
func secretKeyField() TemplateField {
	return TemplateField{Key: "secret_access_key", Type: FieldPassword, Required: true, configKey: "secret_access_key"}
}

// bucketField 通用存储桶字段;值落入渠道 path(渠道内根路径),可带 /前缀 限定子目录。
func bucketField() TemplateField {
	return TemplateField{Key: "bucket", Type: FieldText, Required: true, configKey: "path"}
}

// publicBaseURLField 通用公开访问地址字段(可选);配置后该渠道可 302 直链。
func publicBaseURLField() TemplateField {
	return TemplateField{Key: "public_base_url", Type: FieldText, configKey: "public_base_url"}
}

// Templates 返回内置模板清单(供 API 下发)。
// @return 模板切片
func Templates() []Template { return templates }

// TemplateByID 按标识查内置模板。
// @param templateID 模板标识
// @return 模板副本
// @return 模板是否存在
func TemplateByID(templateID string) (Template, bool) {
	tpl := findTemplate(templateID)
	if tpl == nil {
		return Template{}, false
	}
	return *tpl, true
}

// BuildTemplateConfig 按模板与用户输入合并出渠道 config,并做必填校验。
// @Description: 合并顺序为固定预设 → 字段值(按 configKey 落位)→ endpoint 拼接;
// 参与拼接的中间字段(region/account_id)若 configKey 为空则不直接进 config。
// 密码类值保持明文、哨兵值原样保留,由渠道管理器还原哨兵后再编码。
// @param templateID 模板标识
// @param inputs 用户填写的字段值(键为 TemplateField.Key)
// @return rclone 后端名
// @return 合并后的渠道 config(含约定键 path)
// @return ErrUnknownTemplate 或字段必填校验失败的 ErrInvalidChannel
func BuildTemplateConfig(templateID string, inputs map[string]string) (string, map[string]string, error) {
	tpl := findTemplate(templateID)
	if tpl == nil {
		return "", nil, ErrUnknownTemplate
	}
	config, err := mergeTemplate(tpl, inputs, func(f TemplateField) (string, error) {
		return templateFieldValue(f, inputs)
	})
	if err != nil {
		return "", nil, err
	}
	return tpl.Driver, config, nil
}

// mergeTemplate 按模板合并出后端参数:固定预设 → 字段值(按 configKey 落位)→ endpoint 拼接。
// @param tpl 模板
// @param inputs 用户填写的字段值,供 endpoint 拼接
// @param value 取一个字段的规范值;空串表示该字段不落配置
// @return 合并后的参数
// @return value 返回的错误
func mergeTemplate(tpl *Template, inputs map[string]string,
	value func(TemplateField) (string, error)) (map[string]string, error) {
	config := maps.Clone(tpl.preset)
	if config == nil {
		config = map[string]string{}
	}
	for _, f := range tpl.Fields {
		v, err := value(f)
		if err != nil {
			return nil, err
		}
		if f.configKey != "" && v != "" {
			config[f.configKey] = v
		}
	}
	if tpl.endpointPattern != "" {
		config["endpoint"] = renderEndpoint(tpl, inputs)
	}
	return config, nil
}

// templateFieldValue 取一个字段的规范值:去首尾空白并做必填校验;授权令牌字段归一化并校验令牌格式。
// @Description: 授权令牌可能是一键授权的产物,也可能是用户粘贴的 rclone authorize 输出;
// 哨兵值表示沿用原凭证,原样返回。
// @param f 模板字段
// @param inputs 用户填写的字段值
// @return 规范值,可为空串
// @return 必填缺失或令牌格式非法时的错误
func templateFieldValue(f TemplateField, inputs map[string]string) (string, error) {
	v := strings.TrimSpace(inputs[f.Key])
	if f.Required && v == "" {
		return "", fmt.Errorf("%w: 字段 %s 必填", ErrInvalidChannel, f.Key)
	}
	if f.Type != FieldOAuth || v == "" || v == SecretPlaceholder {
		return v, nil
	}
	token, err := ParsePastedToken(v)
	if err != nil {
		// 哨兵留在链首,面向用户的文案去掉包前缀后仍从「渠道参数非法」读起。
		return "", fmt.Errorf("%w(字段 %s)", err, f.Key)
	}
	return token, nil
}

// renderEndpoint 把各字段值代入模板的 endpoint 拼接模式。
// @param tpl 模板,endpointPattern 非空
// @param inputs 用户填写的字段值
// @return 拼接后的 endpoint
func renderEndpoint(tpl *Template, inputs map[string]string) string {
	ep := tpl.endpointPattern
	for _, f := range tpl.Fields {
		ep = strings.ReplaceAll(ep, "{"+f.Key+"}", strings.TrimSpace(inputs[f.Key]))
	}
	return ep
}

// findTemplate 按标识查内置模板。
// @param templateID 模板标识
// @return 命中的模板;不存在返回 nil
func findTemplate(templateID string) *Template {
	for i := range templates {
		if templates[i].ID == templateID {
			return &templates[i]
		}
	}
	return nil
}

// TemplateInputsFromConfig 从渠道 config 反解出模板表单字段值(供编辑表单预填)。
// @Description: 与 BuildTemplateConfig 互逆——直落 configKey 的字段取同键值,
// 参与 endpoint 拼接的字段按模板 endpoint 模式反向提取;落在敏感键上的字段(密码、
// 授权令牌、访问密钥 ID 等)以 SecretPlaceholder 掩码,明文不下发。
// @param templateID 模板标识
// @param config 解密后的渠道 config
// @param secret 须保密的键(见 secretKeysOf)
// @return 表单字段值(键为 TemplateField.Key)
// @return 模板是否存在
func TemplateInputsFromConfig(templateID string, config map[string]string,
	secret map[string]bool) (map[string]string, bool) {
	tpl := findTemplate(templateID)
	if tpl == nil {
		return nil, false
	}
	inputs := make(map[string]string, len(tpl.Fields))
	for _, f := range tpl.Fields {
		switch {
		// 前端据授权字段的哨兵显示「已授权」。
		case secret[f.configKey]:
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
		default:
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
