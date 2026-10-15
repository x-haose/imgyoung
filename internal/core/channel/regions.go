package channel

// 各厂商的区域可选值(下拉选项),取主流子集;显示名由前端按值查词典。
// 非完整列表——厂商开区随时间变化,用户也可走「S3 兼容(通用)」模板自填端点。

// awsRegions AWS S3 区域;值作 rclone region 参数,endpoint 留空由 region 推导。
var awsRegions = []string{
	"us-east-1", "us-east-2", "us-west-1", "us-west-2", "eu-west-1",
	"eu-central-1", "ap-southeast-1", "ap-northeast-1", "ap-east-1", "ap-southeast-2",
}

// cosRegions 腾讯云 COS 区域;值拼进 endpoint(cos.{值}.myqcloud.com)。
var cosRegions = []string{
	"ap-beijing", "ap-shanghai", "ap-guangzhou", "ap-chengdu", "ap-nanjing",
	"ap-hongkong", "ap-singapore", "ap-tokyo", "na-siliconvalley", "eu-frankfurt",
}

// ossRegions 阿里云 OSS 区域;值含 oss- 前缀,拼进 endpoint({值}.aliyuncs.com)。
var ossRegions = []string{
	"oss-cn-hangzhou", "oss-cn-shanghai", "oss-cn-beijing", "oss-cn-zhangjiakou", "oss-cn-shenzhen",
	"oss-cn-guangzhou", "oss-cn-hongkong", "oss-ap-southeast-1", "oss-ap-northeast-1", "oss-us-west-1",
	"oss-eu-central-1",
}
