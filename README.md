# ImgYoung

多渠道主备容灾的自部署图床(Go + rclone)。**开发中:当前为迭代 0 骨架**。

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
# 上传
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

```yaml
# imgyoung.yml 示例
listen: ":8080"
data_dir: /srv/imgyoung
token: my-token
max_upload_mb: 50
```

## 开发

```bash
task test   # 全量测试(-race)
task lint   # golangci-lint,0 issue 门禁
task --list # 查看全部任务
```
