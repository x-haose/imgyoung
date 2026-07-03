package db

// DefaultUserID 是迁移播种的默认用户 id(设计定稿·鉴权模型:默认单用户单 Token)。
// 未接入多用户前,所有上传引用归属该用户;迁移里的种子值与此常量必须一致。
// 迭代 5 建号后,本常量随鉴权逻辑迁往 core/auth。
const DefaultUserID = "default"
