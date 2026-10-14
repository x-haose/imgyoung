# 前端阶段:构建管理后台产物,交给下方 go:embed
FROM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable pnpm
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 构建阶段:纯 Go 静态编译(内嵌前端产物),并预创建数据目录(属主 nonroot)
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/imgyoung ./cmd/imgyoung \
    && mkdir -p /out/data && chown 65532:65532 /out/data

# 运行阶段:distroless 静态基底(无 shell、无包管理器,最小攻击面),nonroot 运行
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/imgyoung /imgyoung
# /data 必须在镜像内先以 nonroot 属主存在,否则匿名卷挂载后 uid 65532 无写权限
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV IMGYOUNG_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8721
ENTRYPOINT ["/imgyoung"]
