# ImgYoung

多渠道主备容灾的自部署图床(Go + rclone)。**开发中:迭代 6(按需处理 + 查重)与迭代 5(管理后台 + 多用户)已完成**。

已具备:**多渠道主备容灾**(优先级选渠道 · 健康检查自动切换 · 异步副本补齐 · 访问即时容灾)· **渠道存储加密**(rclone crypt,渠道商只见密文)· **Web 管理后台**(多用户 · 具名 Token · 配额/频控 · 查重与冷图裁决 · 审计 · 数据导出)· 按需处理(`?w=` 缩放 + WebP 协商)· 后台查重(精确自动合并 / 相似人工裁决)· TTL 生命周期 · magic bytes 格式校验 · JPEG 元数据剥离 · 像素预算 · SHA256 + 感知哈希 · 客户端幂等 · 防盗链 · 代理/302 双模式。

设计蓝图:[图床项目_功能与设计定稿.md](./图床项目_功能与设计定稿.md) · 项目约定:[CLAUDE.md](./CLAUDE.md)

## 快速开始

```bash
# 本地运行(需要 https://taskfile.dev 与 pnpm;build 会先构建后台前端)
task build && IMGYOUNG_ADMIN_PASSWORD=change-me IMGYOUNG_TOKEN=my-token ./imgyoung

# 或 Docker
docker build -t imgyoung .
docker run -p 8080:8080 -v imgyoung-data:/data \
  -e IMGYOUNG_ADMIN_PASSWORD=change-me -e IMGYOUNG_TOKEN=my-token imgyoung
```

```bash
# 上传(可选:-H "Idempotency-Key: xx" 幂等重试;-F exif_policy=strip 剥离 EXIF)
curl -H "Authorization: Bearer my-token" -F "file=@a.png" http://localhost:8080/api/upload
# => {"id":"R0RjUyP1UW","url":"/i/R0RjUyP1UW"}

# 访问(?w=400 档位缩放,浏览器支持时自动 WebP)
open http://localhost:8080/i/R0RjUyP1UW
```

## 管理后台

浏览器打开 `http://localhost:8080/`,以 `admin` + `IMGYOUNG_ADMIN_PASSWORD` 登录。后台提供:总览仪表盘、图片管理、Token 签发与吊销、存储渠道管理、多用户建号与配额、疑似重复并排裁决、冷图待清理裁决、审计日志、元数据导出。

认证面只有两个:**后台走密码 + session**(argon2id / CSRF / 登录限速),**数据 API 走具名 Token**(`Authorization: Bearer`,DB 只存哈希,可吊销)。`IMGYOUNG_TOKEN` 是引导 Token:设置后首启幂等植入为管理员名下的 `bootstrap` Token,之后随时可在后台吊销、另签新 Token——真相在 DB,环境变量只是种子。

## 配置

三层优先级(由低到高):**内嵌默认 `config.yml`(编译进二进制)< 外部配置文件 < 环境变量**。外部配置文件通过 `IMGYOUNG_CONFIG` 指定,或自动发现工作目录下的 `configs/config.yml`;两者都缺省时靠内嵌默认兜底,单文件即可运行。

| 环境变量 | yaml 键 | 默认 | 说明 |
|---|---|---|---|
| `IMGYOUNG_LISTEN` | `listen` | `:8080` | HTTP 监听地址 |
| `IMGYOUNG_DATA_DIR` | `data_dir` | `data` | 数据目录(SQLite 与本地存储) |
| `IMGYOUNG_TOKEN` | `token` | (空) | 引导 Token:首启幂等植入为管理员的 `bootstrap` Token;留空则仅经后台签发 |
| `IMGYOUNG_ADMIN_PASSWORD` | `admin_password` | (空) | 管理员初始密码,仅在尚未设密时生效;之后以后台修改为准 |
| `IMGYOUNG_UPLOAD_RATE_PER_MIN` | `upload_rate_per_min` | `0` | 每用户每分钟上传次数上限,超出 429;`0` = 不启用 |
| `IMGYOUNG_DEDUP_PHASH_MAX_DISTANCE` | `dedup_phash_max_distance` | `10` | pHash 相似判定的汉明距离上限;`0` = 不做相似扫描 |
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
| `IMGYOUNG_BACKUP_INTERVAL_HOURS` | `backup_interval_hours` | `24` | 元数据库自动备份间隔(小时);快照上传到最高优先级健康渠道 |
| `IMGYOUNG_BACKUP_KEEP` | `backup_keep` | `7` | 备份保留份数,超出的旧快照自动清理 |
| `IMGYOUNG_EXIF_POLICY` | `exif_policy` | `passthrough` | 全局默认 EXIF 档位:`passthrough`(原样保留)/ `strip`(剥离元数据);`spoof` 为实验特性尚未开放。单次上传可用表单字段 `exif_policy` 覆盖 |

```yaml
# configs/config.yml 示例(只需写要覆盖的项)
listen: ":8080"
data_dir: /srv/imgyoung
token: my-token
max_upload_mb: 50
```

## 渠道管理

渠道 = 一个 rclone 后端实例。上传写主渠道(健康渠道中优先级最高者)即返回,其余渠道异步补齐;访问按优先级容灾切换。

渠道在 **Web 后台「存储渠道」页**创建与删除(管理面走 session 认证,上传 Token 无权操作)。创建时填 rclone 驱动名与后端参数(`path` 为后端内路径,`public_base_url` 为 302 直链前缀,其余原样传给 rclone):

```text
# S3 / R2 渠道参数示例
driver:   s3
config:   provider=Cloudflare  access_key_id=...  secret_access_key=...
          endpoint=https://xxx.r2.cloudflarestorage.com  path=bucket/imgs
# 删除守卫:渠道持有某些图的唯一副本时拒绝删除(409),防止数据丢失
```

- `encrypted: true` 的渠道经 rclone crypt 存储:内容与文件名均为密文,渠道商不可见;密码由主密钥派生,持 `master.key` 可用标准 rclone 工具独立解密(自救)。
- 加密设置创建时固化,建后不可变(变更会使既有对象不可读)。
- **S3 兼容后端(R2/MinIO 等)请预先创建好 bucket**:图床默认对 s3 渠道置 `no_check_bucket=true`,跳过 rclone 上传前的建桶探测——作用域凭证通常无建桶权限,否则表现为"健康探测通过但上传 403"。确需 rclone 自动建桶,在 config 显式加 `"no_check_bucket":"false"`。

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
task e2e    # 端到端测试:编译真实二进制、启动进程、覆盖全部对外功能
task lint   # golangci-lint,0 issue 门禁
task --list # 查看全部任务
```
