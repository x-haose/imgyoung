# 图样 ImgYoung

多渠道主备容灾的自部署图床。一张图同时存进多个存储渠道，哪个渠道出了问题，上传和访问都照常进行。

A self-hosted image host with multi-channel failover: every image is stored in several storage channels, and uploads and reads keep working when one of them fails.

![ImgYoung](https://raw.githubusercontent.com/x-haose/imgyoung/main/docs/public/screenshots/dashboard-zh-light.webp)

## 快速开始 / Quick start

```bash
docker run -d --name imgyoung --restart unless-stopped \
  -p 8721:8721 -v imgyoung-data:/data haose/imgyoung:latest
```

浏览器打开 `http://<主机>:8721/`，用 `admin` 和启动日志里的初始密码登录（`docker logs imgyoung 2>&1 | grep 初始密码`），首次登录会要求修改密码。

Open `http://<host>:8721/` and sign in as `admin` with the initial password printed in the first start's log. The first sign-in forces a password change.

**数据都在 `/data` 卷里。启动后请单独备份其中的 `master.key`，丢失后渠道凭证与加密数据无法恢复。**

Everything lives in the `/data` volume. Back up `master.key` separately; without it, channel credentials and encrypted data cannot be recovered.

## 标签 / Tags

| 标签 | 含义 |
|---|---|
| `x.y.z` | 具体版本，发布后不再变化 / an exact version that never changes |
| `x.y` | 该次版本系列的最新版 / the latest release of that minor series |
| `latest` | 最新正式版 / the latest stable release |

镜像同时发布在 GHCR：`ghcr.io/x-haose/imgyoung`。支持 `linux/amd64` 与 `linux/arm64`。

The image is also published on GHCR as `ghcr.io/x-haose/imgyoung`, for `linux/amd64` and `linux/arm64`.

## 链接 / Links

- 文档 Docs：https://imgyoung.x-haose.com/zh/docs
- NAS 与服务器面板部署 NAS and panels：https://imgyoung.x-haose.com/zh/docs/install/nas-panel
- 源码 Source：https://github.com/x-haose/imgyoung
- 许可证 License：MIT
