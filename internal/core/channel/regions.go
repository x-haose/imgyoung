package channel

// 各厂商的区域清单(下拉选项),取主流子集;label 为中文名 + code,value 为 rclone 用值。
// 非完整列表——厂商开区随时间变化,用户也可走「S3 兼容(通用)」自填。

// awsRegions AWS S3 区域;value 作 rclone region 字段,endpoint 留空由 region 推导。
var awsRegions = []FieldOption{
	{Label: "美国东部(弗吉尼亚北部) us-east-1", Value: "us-east-1"},
	{Label: "美国东部(俄亥俄) us-east-2", Value: "us-east-2"},
	{Label: "美国西部(加利福尼亚北部) us-west-1", Value: "us-west-1"},
	{Label: "美国西部(俄勒冈) us-west-2", Value: "us-west-2"},
	{Label: "欧洲(爱尔兰) eu-west-1", Value: "eu-west-1"},
	{Label: "欧洲(法兰克福) eu-central-1", Value: "eu-central-1"},
	{Label: "亚太(新加坡) ap-southeast-1", Value: "ap-southeast-1"},
	{Label: "亚太(东京) ap-northeast-1", Value: "ap-northeast-1"},
	{Label: "亚太(香港) ap-east-1", Value: "ap-east-1"},
	{Label: "亚太(悉尼) ap-southeast-2", Value: "ap-southeast-2"},
}

// cosRegions 腾讯云 COS 区域;value 拼进 endpoint(cos.{value}.myqcloud.com)。
var cosRegions = []FieldOption{
	{Label: "北京 ap-beijing", Value: "ap-beijing"},
	{Label: "上海 ap-shanghai", Value: "ap-shanghai"},
	{Label: "广州 ap-guangzhou", Value: "ap-guangzhou"},
	{Label: "成都 ap-chengdu", Value: "ap-chengdu"},
	{Label: "南京 ap-nanjing", Value: "ap-nanjing"},
	{Label: "中国香港 ap-hongkong", Value: "ap-hongkong"},
	{Label: "新加坡 ap-singapore", Value: "ap-singapore"},
	{Label: "日本(东京) ap-tokyo", Value: "ap-tokyo"},
	{Label: "美国西部(硅谷) na-siliconvalley", Value: "na-siliconvalley"},
	{Label: "欧洲(法兰克福) eu-frankfurt", Value: "eu-frankfurt"},
}

// ossRegions 阿里云 OSS 区域;value 含 oss- 前缀,拼进 endpoint({value}.aliyuncs.com)。
var ossRegions = []FieldOption{
	{Label: "华东1(杭州) oss-cn-hangzhou", Value: "oss-cn-hangzhou"},
	{Label: "华东2(上海) oss-cn-shanghai", Value: "oss-cn-shanghai"},
	{Label: "华北2(北京) oss-cn-beijing", Value: "oss-cn-beijing"},
	{Label: "华北3(张家口) oss-cn-zhangjiakou", Value: "oss-cn-zhangjiakou"},
	{Label: "华南1(深圳) oss-cn-shenzhen", Value: "oss-cn-shenzhen"},
	{Label: "华南3(广州) oss-cn-guangzhou", Value: "oss-cn-guangzhou"},
	{Label: "中国香港 oss-cn-hongkong", Value: "oss-cn-hongkong"},
	{Label: "亚太东南1(新加坡) oss-ap-southeast-1", Value: "oss-ap-southeast-1"},
	{Label: "亚太东北1(东京) oss-ap-northeast-1", Value: "oss-ap-northeast-1"},
	{Label: "美国西部1(硅谷) oss-us-west-1", Value: "oss-us-west-1"},
	{Label: "欧洲中部1(法兰克福) oss-eu-central-1", Value: "oss-eu-central-1"},
}
