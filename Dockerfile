# 构建阶段:纯 Go 静态编译,并预创建数据目录(属主 nonroot)
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/imgyoung ./cmd/imgyoung \
    && mkdir -p /out/data && chown 65532:65532 /out/data

# 运行阶段:distroless 静态基底(无 shell、无包管理器,最小攻击面),nonroot 运行
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/imgyoung /imgyoung
# /data 必须在镜像内先以 nonroot 属主存在,否则匿名卷挂载后 uid 65532 无写权限
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV IMGYOUNG_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/imgyoung"]
