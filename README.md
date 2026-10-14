# ImgYoung

[English](./README.en.md)

多渠道主备容灾的自部署图床(Go + rclone)。**开发中:迭代 0–7 已完成,管理后台已重做;正在做 v1.0 发布前的收口。**

已具备:**多渠道主备容灾**(优先级选渠道 · 健康检查自动切换 · 异步副本补齐 · 访问即时容灾)· **渠道存储加密**(rclone crypt,渠道商只见密文)· **Web 管理后台**(多用户 · 具名 Token · 配额/频控 · 查重与冷图裁决 · 审计 · 数据导出)· 按需处理(`?w=` 缩放 + WebP 协商)· 后台查重(精确自动合并 / 相似人工裁决)· TTL 生命周期 · magic bytes 格式校验 · 元数据剥离(JPEG/PNG/WebP) · 像素预算 · SHA256 + 感知哈希 · 客户端幂等 · 防盗链 · 代理/302 双模式。

设计蓝图:[图床项目_功能与设计定稿.md](./图床项目_功能与设计定稿.md) · 项目约定:[CLAUDE.md](./CLAUDE.md)

## 快速开始

三种安装方式任选,首启都无需任何环境变量:用 `admin` + 出厂默认密码 `imgyoung123` 登录后台,首次登录强制改密。默认监听 `:8721`(避开 8080 等易冲突端口),可用 `IMGYOUNG_LISTEN` 覆盖。

```bash
# 1. 发布版二进制:到 Releases 下载对应平台的压缩包(linux / macOS / Windows,amd64 与 arm64),解压即用
#    https://github.com/x-haose/imgyoung/releases
./imgyoung            # 数据落在当前目录的 data/,可用 IMGYOUNG_DATA_DIR 指定
./imgyoung -version   # 查看版本、提交与构建时刻

# 2. Docker 镜像(多架构;latest 指向最新正式版,也可钉 1.0 或具体版本)
docker run -p 8721:8721 -v imgyoung-data:/data ghcr.io/x-haose/imgyoung:latest

# 3. 从源码(需要 https://taskfile.dev 与 pnpm;build 会先构建后台前端)
task run
# go install github.com/x-haose/imgyoung/cmd/imgyoung@latest 也能装,但前端产物不入库,
# 这样装出来的二进制只有 API 没有后台页面;要后台请用前两种方式或 task build。
```

浏览器打开 `http://localhost:8721/`,用 `admin` + 默认密码 `imgyoung123` 登录(首次登录会要求你立即改密),进后台「上传 Token」页签发一个 Token,即可用它上传:

```bash
# 上传(可选:-H "Idempotency-Key: xx" 幂等重试;-F exif_policy=strip 剥离 EXIF)
curl -H "Authorization: Bearer <后台签发的Token>" -F "file=@a.png" http://localhost:8721/api/upload
# => {"id":"R0RjUyP1UW","url":"/i/R0RjUyP1UW"}

# 从别的图床转存一张(服务端替你抓,仅限公网地址)
curl -H "Authorization: Bearer <后台签发的Token>" -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/photo.png"}' http://localhost:8721/api/upload/url

# 访问(?w=400 档位缩放,浏览器支持时自动 WebP)
open http://localhost:8721/i/R0RjUyP1UW
```

### 用 PicGo / Typora 等客户端

这些客户端大多内置「Amazon S3」选项,本站同时以 S3 协议收图,选它即可、不必装插件:到后台
「上传 Token」页点某枚 Token 的 **「S3 配置」**,把弹窗里的 access key / secret / 端点 / 自定义域名
照填进客户端(勾上「路径风格」)。凭证就是这枚 Token——吊销它,S3 那条路同时失效。

客户端自取的对象键会成为访问别名,`/o/<用户 id>/<对象键>` 与短链 `/i/<短ID>` 指向同一张图,
所以客户端贴出来的 Markdown 链接直接可用。同键重传按 S3 语义覆盖:别名指向新图,旧图短链仍有效。

S3 面实现 `PutObject`、`HeadBucket`、`HeadObject`、`GetObject`、`DeleteObject` 五个操作,够上传、校验、取回与删除
自己的对象;列桶、分段上传、ACL 等不提供。公开访问与管理仍走 `/i/`、`/o/` 与后台。

## 管理后台

浏览器打开 `http://localhost:8721/`,以 `admin` + 初始密码登录。后台提供:总览仪表盘(含未恢复告警)、图片管理(含副本分布)、Token 签发与吊销、存储渠道管理、多用户建号与配额、疑似重复并排裁决、冷图待清理裁决、审计日志、设置(运行配置 / 防盗链 / 元数据导出 / 备份快照与恢复 / 告警历史)。界面中英双语,右上角切换语言与主题。

认证面只有两个:**后台走密码 + session**(argon2id / CSRF / 登录限速),**数据 API 走具名 Token**(`Authorization: Bearer`,DB 只存哈希,可吊销;S3 协议面的 SigV4 签名用的也是这批 Token,不是第三套凭证)。管理员首启播种为出厂默认密码 `imgyoung123`(公开值,仅为开箱可登录,登录后强制改密);真相入库,改后一律以库为准。上传 Token 一律在后台签发,不设环境变量托管的引导 Token。

### 忘记密码

密码在库里是 argon2id 哈希、无法手写,但首启播种逻辑是「管理员尚未设密时才播种」——所以清空该用户的 `password_hash` 再重启,进程会当作首启重新播种出厂默认密码 `imgyoung123`:

```bash
# 停服后,对数据目录下的 imgyoung.db 执行(需要 sqlite3 或任意 SQLite 工具):
sqlite3 data/imgyoung.db "UPDATE users SET password_hash='' WHERE name='admin';"
```

重启服务,用 `admin` + `imgyoung123` 登录,系统会要求你立即改成新密码。

## API 文档

全部 HTTP 接口以 OpenAPI 3.1 描述在 [api/openapi.yaml](./api/openapi.yaml);运行中的实例在 `GET /api/openapi.yaml` 提供与自身版本一致的副本,可直接喂给 Scalar、Swagger UI 或任何代码生成器。端到端测试按这份文档校验每条响应,管理后台的接口类型也由它生成,文档与实现不会各说各话。S3 协议面按 AWS 规范实现,见上文。

## 配置

配置一刀切两类,规则一句话:**环境变量只管把进程启动起来,运营参数一律后台改、即时生效**。不设外部配置文件、不引 koanf。

**引导配置**(只认环境变量,改动需重启):

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `IMGYOUNG_LISTEN` | `:8721` | HTTP 监听地址 |
| `IMGYOUNG_DATA_DIR` | `data` | 数据目录(SQLite 与本地存储) |
| `IMGYOUNG_MASTER_KEY` | (自动生成 `data/master.key`) | 主密钥(64 位 hex),加密渠道凭证与派生渠道加密密码;**丢失则渠道配置与密文数据不可恢复** |
| `IMGYOUNG_TRUSTED_PROXIES` | `127.0.0.0/8,::1/128` | 信任其转发头的反向代理网段(逗号分隔 CIDR 或 IP)。连接对端在网段内时才采信 `X-Forwarded-For` / `X-Forwarded-Proto` / `X-Forwarded-Host`:登录限速按真实客户端计、HTTPS 时会话 cookie 带 Secure、未配 `base_url` 时按对外域名推导地址。反代不在同一台机器(如 Docker 网络)时填它的网段 |
| `IMGYOUNG_METRICS_ALLOW` | 回环 + 私网(`10/8`、`172.16/12`、`192.168/16`、`fc00::/7`) | 允许抓取 `/metrics` 的网段,网段外一律 403;指标含路由模板、渠道名与用量,不对公网开放 |

**运营配置**(真相在数据库 `settings` 单行宽表,后台「设置 → 运行配置」修改、保存即热生效):**不接受环境变量**——新实例登录后台配置一次即可,DB 随数据卷持久。下表列出各键与出厂默认(默认值即表列 `DEFAULT`,防盗链白名单在设置页单独管理):

| 配置键 | 默认 | 说明 |
|---|---|---|
| `max_upload_mb` | `50` | 单图上限(MB) |
| `max_pixels_mp` | `50` | 像素预算(百万像素) |
| `max_concurrent_uploads` | `8` | 上传并发上限,满载返回 429 |
| `upload_rate_per_min` | `0` | 每用户每分钟上传次数上限,超出 429;`0` = 不启用 |
| `base_url` | (空) | 对外站点地址;留空按请求域名推导绝对地址,经反代 / CDN 对外域名与请求域名不一致时需显式配置 |
| `access_mode` | `proxy` | `proxy`(代理,容灾即时)/ `redirect`(302 省流量,需渠道允许直链) |
| `min_success` | `1` | 上传返回成功所需的最少同步副本数 |
| `allow_empty_referer` | `true` | 空 Referer 是否放行 |
| `exif_policy` | `passthrough` | 全局默认 EXIF 档位:`passthrough`(原样保留)/ `strip`(剥离元数据);`spoof` 为实验特性尚未开放。单次上传可用表单字段 `exif_policy` 覆盖 |
| `encrypt_new_channels` | `false` | 新建渠道未显式指定时的加密默认值(创建时固化) |
| `default_ttl_hours` | `0` | 全局默认有效期(小时);`0` = 永不过期 |
| `ttl_renew_on_access` | `true` | 访问即续期:公开访问把带有效期图片的到期时刻按其时长顺延 |
| `cold_cleanup_days` | `90` | 冷图待清理阈值(天);`0` = 不启用 |
| `delete_grace_minutes` | `0` | 引用归零到物理删的宽限(分钟);`0` = 立即 |
| `cleanup_sweep_seconds` | `60` | 生命周期清理扫描间隔(秒) |
| `activity_flush_seconds` | `60` | 访问活跃度批量落库间隔(秒) |
| `health_check_seconds` | `60` | 渠道健康检查间隔(秒) |
| `backup_interval_hours` | `24` | 元数据库自动备份间隔(小时);快照上传到最高优先级健康渠道 |
| `backup_keep` | `7` | 备份保留份数,超出的旧快照自动清理 |
| `usage_report_seconds` | `300` | 渠道用量上报间隔(秒) |
| `channel_quota_gb` | `0` | 单渠道用量告警阈值(GB);`0` = 只上报不告警 |
| `dedup_scan_seconds` | `300` | 后台查重扫描间隔(秒) |
| `dedup_phash_max_distance` | `10` | pHash 相似判定的汉明距离上限;`0` = 不做相似扫描 |
| `process_widths` | `100,200,400,800,1600` | 按需处理允许的宽度档(像素);清空 = 不允许缩放 |
| `process_formats` | `webp,jpeg,png` | 按需处理允许的目标格式 |
| `process_quality_webp` | `80` | webp 质量档 [0,100] |
| `process_quality_jpeg` | `85` | jpeg 质量档 [1,100] |
| `process_webp_method` | `4` | webp 质量/速度权衡 [0,6] |
| `process_workers` | `4` | 处理并发上限 |
| `process_timeout_ms` | `5000` | 单请求处理超时(毫秒),超时回退原图 |
| `variants_cache_mb` | `10240` | 衍生图磁盘缓存上限(MB),超过按最近访问 LRU 淘汰 |
| `idempotency_key_ttl_hours` | `24` | 上传幂等键保留期(小时):客户端带 `Idempotency-Key` 重试的有效窗口,超期的键由清理循环回收 |
| `cdn_cloudflare_zone_id` | (空) | Cloudflare Zone ID;与 `cdn_cloudflare_api_token`、`base_url` 三者齐备时,删除图片级联清除 CDN 缓存 |
| `cdn_cloudflare_api_token` | (空) | Cloudflare API Token,需 Zone.Cache Purge 权限;加密入库,不进日志与任何响应 |
| `alert_webhook_url` | (空) | 告警开启 / 恢复时以 JSON POST 到该地址(http 或 https);留空 = 只记录不外发 |
| `audit_retention_days` | `0` | 审计日志保留天数,超期条目由清理循环回收;`0` = 永久保留 |

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

- 删除渠道默认**同时清理渠道上的对象**(后台逐个删除,期间渠道显示为「清理中」,删完自动移除);凭证已失效或桶已不存在时选「仅移除」,远端对象原样保留。持有任何图唯一副本的渠道会被拒绝删除。
- `encrypted: true` 的渠道经 rclone crypt 存储:内容与文件名均为密文,渠道商不可见;密码由主密钥派生,持 `master.key` 可用标准 rclone 工具独立解密(自救)。
- 加密设置创建时固化,建后不可变(变更会使既有对象不可读)。
- **S3 兼容后端(R2/MinIO 等)请预先创建好 bucket**:图床默认对 s3 渠道置 `no_check_bucket=true`,跳过 rclone 上传前的建桶探测——作用域凭证通常无建桶权限,否则表现为"健康探测通过但上传 403"。确需 rclone 自动建桶,在 config 显式加 `"no_check_bucket":"false"`。

## 已知限制

- 动画 WebP 暂不支持(解码库限制,上传返回 400);动画 GIF 正常。
- `exif_policy=strip` 覆盖全部支持格式:JPEG 剥离元数据段与 EOI 后的尾部附加数据(如 Motion Photo 视频),PNG 剥离 eXIf 与文本块及 IEND 后的尾部数据,WebP 剥离 EXIF/XMP chunk 并同步清 VP8X 标志位;GIF 无标准元数据容器,直通。

## PicGo / PicList 对接

使用 [picgo-plugin-web-uploader](https://github.com/squirrel-land/picgo-plugin-web-uploader) 或 PicList 自定义图床,按以下参数配置:

| 参数 | 值 |
|---|---|
| API 地址 | `https://你的域名/api/upload`(返回的 `url` 默认按请求域名推导为绝对地址;经反代 / CDN 对外域名与请求域名不一致时需设置 `base_url`) |
| POST 参数名 | `file` |
| 自定义请求头 | `{"Authorization": "Bearer 你的Token"}` |
| JSON 路径 | `url` |

## 作为 Go 库使用

存储引擎是公开包:`store/driver` 定义 `Driver` 接口(写入 / 读取 / 查询 / 删除 / 列举),`store/rclone` 用 rclone 的任一后端实现它,并可叠加 crypt 加密层;两者都不依赖本项目的其它代码。

```go
import (
	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/rclone"
)

d, err := rclone.Open(ctx, rclone.Options{
	Backend: "s3", // 任一 rclone 后端名
	Params:  map[string]string{"provider": "Cloudflare", "access_key_id": "...", "secret_access_key": "..."},
	Path:    "bucket/prefix",
	Crypt:   &rclone.CryptOptions{Password: "...", Salt: "..."}, // 可选:内容与文件名加密
})
```

密码类参数先用 `rclone.ObscureSecrets` 按 rclone 的要求编码;对象缺失统一返回 `driver.ErrNotFound`。完整示例见 [store/rclone 的 Example](./store/rclone/example_test.go)。

## 开发

```bash
task test   # 全量测试(-race)
task e2e    # 端到端测试:编译真实二进制、启动进程、覆盖全部对外功能,并按 OpenAPI 文档校验每条响应
task lint   # golangci-lint + 后台前端的 tsc / eslint / prettier + 接口文档 lint,0 issue 门禁
task --list # 查看全部任务
```

环境、约定、提交与发布流程见 [CONTRIBUTING.md](./CONTRIBUTING.md)。推送 `vX.Y.Z` 标签即触发发布工作流:goreleaser 构建六平台二进制、GHCR 多架构镜像与 GitHub Release。

## 许可证

[MIT](./LICENSE)。管理后台复用了 [shadcn-admin](https://github.com/satnaing/shadcn-admin) 的通用组件,其许可证见 [web/licenses/shadcn-admin.LICENSE](./web/licenses/shadcn-admin.LICENSE)。
