# ImgYoung

多渠道主备容灾的自部署图床(Go + rclone)。**开发中:迭代 1(上传链路)进行中**。

已具备:magic bytes 格式校验(jpeg/png/gif/webp 白名单)· JPEG 元数据剥离(EXIF/XMP/IPTC/COM,保留 ICC)· 像素预算 · SHA256 + 感知哈希入库 · 客户端幂等(`Idempotency-Key` 头)· 防盗链白名单 · 代理访问。

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
| `IMGYOUNG_BASE_URL` | `base_url` | (空) | 对外站点地址;设置后上传响应返回绝对 URL(PicGo 等客户端需要) |
| `IMGYOUNG_REFERER_WHITELIST` | `referer_whitelist` | (空 = 不启用) | 防盗链白名单,逗号分隔,支持 `*.example.com` |
| `IMGYOUNG_ALLOW_EMPTY_REFERER` | `allow_empty_referer` | `true` | 空 Referer 是否放行 |
| `IMGYOUNG_EXIF_POLICY` | `exif_policy` | `passthrough` | 全局默认 EXIF 档位:`passthrough`(原样保留)/ `strip`(剥离元数据);`spoof` 为实验特性尚未开放。单次上传可用表单字段 `exif_policy` 覆盖 |

```yaml
# imgyoung.yml 示例
listen: ":8080"
data_dir: /srv/imgyoung
token: my-token
max_upload_mb: 50
```

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
