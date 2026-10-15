# ImgYoung

[English](./README.en.md) · [文档](https://imgyoung.x-haose.com/zh/docs) · [发布版本](https://github.com/x-haose/imgyoung/releases)

多渠道主备容灾的自部署图床。一张图同时存进多个存储渠道（对象存储、网盘、本地磁盘，任何 rclone 后端），
某个渠道挂了，上传与访问自动切到下一个；渠道可以加密到只剩密文，元数据库定时快照到渠道里。单二进制，Docker 一行起。

## 特性

- **多渠道主备容灾**：优先级表达主备，健康探测自动降级；上传失败顺延下一渠道，可要求同步写够 N 份；副本补齐任务持久化，进程重启不丢。
- **渠道存储加密**：基于 rclone crypt，渠道商只见密文；密钥由主密钥派生，标准 rclone 工具可独立解密。
- **元数据库自动备份**：定时快照上传到每个健康渠道，后台可浏览快照并一键恢复。
- **生命周期**：有效期三层可设、访问即续期；长期无人访问的冷图进待清理列表，由管理员裁决。
- **后台查重**：字节相同的图自动合并；感知哈希相似的图并排给管理员裁决。
- **多种上传入口**：HTTP API、PicGo / PicList、S3 协议、URL 转存、后台拖拽粘贴，共用一条上传管道。
- **按需处理**：`?w=` 按预设宽度缩放，按浏览器能力返回 WebP；原图 URL 不变，适合交给 CDN 长期缓存。
- **管理后台**：多用户、具名 Token、配额与频控、审计、告警、元数据导出；中英双语，暗色 / 亮色主题。

## 快速开始

```bash
docker run -d --name imgyoung --restart unless-stopped \
  -p 8721:8721 -v imgyoung-data:/data ghcr.io/x-haose/imgyoung:latest
```

Linux / macOS 也可以一行装成本机程序（加 `sh -s -- --service` 同时注册 systemd 服务）：

```bash
curl -fsSL https://raw.githubusercontent.com/x-haose/imgyoung/main/install.sh | sh
```

浏览器打开 `http://localhost:8721/`，用 `admin` / `imgyoung123` 登录（首次登录要求改密），在「上传 Token」页签发一枚 Token，即可上传：

```bash
curl -H "Authorization: Bearer <Token>" -F "file=@a.png" http://localhost:8721/api/upload
# {"id":"R0RjUyP1UW","url":"http://localhost:8721/i/R0RjUyP1UW"}
```

新实例只有内置的本地渠道，图和备份都在同一块盘上；到「存储渠道」页加一个异地渠道，容灾才真正生效。

## 文档

完整文档在 <https://imgyoung.x-haose.com>：

| 章节 | 内容 |
| --- | --- |
| [安装](https://imgyoung.x-haose.com/zh/docs/install) | 一行脚本、Docker、Docker Compose、发布包、源码构建 |
| [存储渠道](https://imgyoung.x-haose.com/zh/docs/channels) | 主备与补齐、对象存储与 WebDAV、网盘授权、渠道加密 |
| [上传客户端](https://imgyoung.x-haose.com/zh/docs/guides/clients) | PicGo / PicList、S3 协议、各类 S3 客户端 |
| [配置](https://imgyoung.x-haose.com/zh/docs/configuration) | 环境变量与后台运营配置 |
| [运维](https://imgyoung.x-haose.com/zh/docs/operations/reverse-proxy) | 反向代理、CDN、备份恢复、升级、忘记密码、监控 |
| [Go 库](https://imgyoung.x-haose.com/zh/docs/library) | 存储驱动与多存储容灾原语 |
| [接口参考](https://imgyoung.x-haose.com/zh/docs/api) | 由 [api/openapi.yaml](./api/openapi.yaml) 生成 |

示例配置（Compose、Caddy、Nginx、systemd）在 [deploy/](./deploy/)；设计与技术选型见[架构](https://imgyoung.x-haose.com/zh/docs/architecture)；后续计划见带 [`roadmap`](https://github.com/x-haose/imgyoung/issues?q=label%3Aroadmap) 标签的 issue。

## 作为 Go 库使用

存储引擎是公开包，可以单独 `go get`：`store/driver` 定义驱动接口，`store/rclone` 用 rclone 的任一后端实现它（可叠加加密层），
`store/failover` 在一组驱动上提供按序写够 N 份、读第一个可读的、健康探测等容灾原语。

```bash
go get github.com/x-haose/imgyoung/store/failover github.com/x-haose/imgyoung/store/rclone
```

用法与示例见 [Go 库文档](https://imgyoung.x-haose.com/zh/docs/library)。

## 开发

```bash
task test   # 全量测试(-race)
task e2e    # 端到端测试:编译真实二进制、启动进程,并按 OpenAPI 文档校验每条响应
task lint   # Go、后台前端、文档站与安装脚本的检查,0 issue 门禁
task docs   # 文档站本地预览
```

环境、约定、提交与发布流程见 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 许可证

[MIT](./LICENSE)。管理后台复用了 [shadcn-admin](https://github.com/satnaing/shadcn-admin) 的通用组件，其许可证见 [web/licenses/shadcn-admin.LICENSE](./web/licenses/shadcn-admin.LICENSE)。
