# ImgYoung

多渠道主备容灾的自部署图床(Go + rclone)。**开发中:迭代 2(多渠道容灾)已完成**。

已具备:**多渠道主备容灾**(优先级选渠道 · 健康检查自动切换 · 异步副本补齐 · 访问即时容灾)· **渠道存储加密**(rclone crypt,渠道商只见密文)· magic bytes 格式校验 · JPEG 元数据剥离(EXIF/XMP/IPTC/COM + 尾部附加数据)· 像素预算 · SHA256 + 感知哈希 · 客户端幂等 · 防盗链 · 代理/302 双模式。

设计蓝图:[图床项目_功能与设计定稿.md](./图床项目_功能与设计定稿.md) · 项目约定:[CLAUDE.md](./CLAUDE.md)

## 快速开始

```bash
# 本地运行(Token 留空则启动时打印随机 Token,需要 https://taskfile.dev)
task build && IMGYOUNG_TOKEN=my-token ./imgyoung

# 或 Docker
docker build -t imgyoung .
docker run -p 8080:8080 -v imgyoung-data:/data -e IMGYOUNG_TOKEN=my-token imgyoung
```

```bash
# 上传(可选:-H "Idempotency-Key: xx" 幂等重试;-F exif_policy=strip 剥离 EXIF)
curl -H "Authorization: Bearer my-token" -F "file=@a.png" http://localhost:8080/api/upload
# => {"id":"R0RjUyP1UW","url":"/i/R0RjUyP1UW"}

# 访问
open http://localhost:8080/i/R0RjUyP1UW
```

## 配置

三层优先级:**环境变量 > 配置文件(yaml)> 默认值**。配置文件通过 `IMGYOUNG_CONFIG` 指定,或自动发现工作目录下的 `imgyoung.yml`。

| 环境变量 | yaml 键 | 默认 | 说明 |
|---|---|---|---|
| `IMGYOUNG_LISTEN` | `listen` | `:8080` | HTTP 监听地址 |
| `IMGYOUNG_DATA_DIR` | `data_dir` | `data` | 数据目录(SQLite 与本地存储) |
| `IMGYOUNG_TOKEN` | `token` | (随机生成) | 上传 Token |
| `IMGYOUNG_MAX_UPLOAD_MB` | `max_upload_mb` | `50` | 单图上限(MB) |
| `IMGYOUNG_MAX_PIXELS_MP` | `max_pixels_mp` | `50` | 像素预算(百万像素) |
| `IMGYOUNG_MAX_CONCURRENT_UPLOADS` | `max_concurrent_uploads` | `8` | 上传并发上限,满载返回 429 |
| `IMGYOUNG_BASE_URL` | `base_url` | (空) | 对外站点地址;设置后上传响应返回绝对 URL(PicGo 等客户端需要) |
| `IMGYOUNG_REFERER_WHITELIST` | `referer_whitelist` | (空 = 不启用) | 防盗链白名单,逗号分隔,支持 `*.example.com` |
| `IMGYOUNG_ALLOW_EMPTY_REFERER` | `allow_empty_referer` | `true` | 空 Referer 是否放行 |
| `IMGYOUNG_MASTER_KEY` | `master_key` | (自动生成 `data/master.key`) | 主密钥(64 位 hex),加密渠道凭证与派生渠道加密密码;**丢失则渠道配置与密文数据不可恢复** |
| `IMGYOUNG_MIN_SUCCESS` | `min_success` | `1` | 上传返回成功所需的最少同步副本数 |
| `IMGYOUNG_ACCESS_MODE` | `access_mode` | `proxy` | `proxy`(代理,容灾即时)/ `redirect`(302 省流量,需渠道允许直链) |
| `IMGYOUNG_ENCRYPT_NEW_CHANNELS` | `encrypt_new_channels` | `false` | 新建渠道未显式指定时的加密默认值(创建时固化) |
| `IMGYOUNG_HEALTH_CHECK_SECONDS` | `health_check_seconds` | `60` | 渠道健康检查间隔(秒) |
| `IMGYOUNG_EXIF_POLICY` | `exif_policy` | `passthrough` | 全局默认 EXIF 档位:`passthrough`(原样保留)/ `strip`(剥离元数据);`spoof` 为实验特性尚未开放。单次上传可用表单字段 `exif_policy` 覆盖 |

```yaml
# imgyoung.yml 示例
listen: ":8080"
data_dir: /srv/imgyoung
token: my-token
max_upload_mb: 50
```

## 渠道管理

渠道 = 一个 rclone 后端实例。上传写主渠道(健康渠道中优先级最高者)即返回,其余渠道异步补齐;访问按优先级容灾切换。

```bash
# 创建渠道(config 中 path 为后端内路径,public_base_url 为 302 直链前缀,其余为 rclone 后端参数)
curl -X POST -H "Authorization: Bearer my-token" -H "Content-Type: application/json" \
  -d '{"name":"backup","driver":"local","priority":50,"encrypted":true,"config":{"path":"/mnt/backup"}}' \
  http://localhost:8080/api/channels

# S3 渠道示例
#   {"name":"r2","driver":"s3","priority":200,
#    "config":{"provider":"Cloudflare","access_key_id":"...","secret_access_key":"...",
#              "endpoint":"https://xxx.r2.cloudflarestorage.com","path":"bucket/imgs"}}

curl -H "Authorization: Bearer my-token" http://localhost:8080/api/channels   # 列表
curl -X DELETE -H "Authorization: Bearer my-token" http://localhost:8080/api/channels/2
# 删除守卫:渠道持有某些图的唯一副本时返回 409,防止数据丢失
```

- `encrypted: true` 的渠道经 rclone crypt 存储:内容与文件名均为密文,渠道商不可见;密码由主密钥派生,持 `master.key` 可用标准 rclone 工具独立解密(自救)。
- 加密设置创建时固化,建后不可变(变更会使既有对象不可读)。

## 已知限制

- 动画 WebP 暂不支持(解码库限制,上传返回 400);动画 GIF 正常。
- `exif_policy=strip` 目前仅处理 JPEG(剥离元数据段与 EOI 后的尾部附加数据,如 Motion Photo 视频);PNG/WebP/GIF 的元数据块暂原样保留,后续版本补齐。

## PicGo / PicList 对接

使用 [picgo-plugin-web-uploader](https://github.com/squirrel-land/picgo-plugin-web-uploader) 或 PicList 自定义图床,按以下参数配置:

| 参数 | 值 |
|---|---|
| API 地址 | `https://你的域名/api/upload`(需设置 `base_url` 以返回绝对地址) |
| POST 参数名 | `file` |
| 自定义请求头 | `{"Authorization": "Bearer 你的Token"}` |
| JSON 路径 | `url` |

## 开发

```bash
task test   # 全量测试(-race)
task lint   # golangci-lint,0 issue 门禁
task --list # 查看全部任务
```
