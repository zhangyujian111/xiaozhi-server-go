# xiaozhi-server-go — WebSocket / WebRTC 语音服务
#
# 注意：本服务依赖 github.com/hraban/opus，它是 libopus 的 CGO 绑定，
# 因此必须 CGO_ENABLED=1 并在构建期安装 opus 开发包。
# 不能使用纯静态编译。

FROM golang:1.25-alpine AS build

# 切换国内镜像（默认 dl-cdn.alpinelinux.org 在 China 极慢）
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories

# build-base: gcc/musl-dev（CGO 必需）
# pkgconfig:  hraban/opus 通过 pkg-config 定位 libopus
# opus-dev / opusfile-dev: 头文件 + .pc 文件
RUN apk add --no-cache build-base pkgconfig opus-dev opusfile-dev

WORKDIR /src

COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY . .

# CGO_ENABLED=1：libopus 绑定必需
# -ldflags "-s -w"：去掉符号表和调试信息，减小体积
RUN CGO_ENABLED=1 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/xiaozhi-server \
      ./cmd/server

FROM alpine:3.20

# opus / opusfile: 运行时动态库（构建产物是动态链接的）
# ca-certificates: 出站 HTTPS 调用 ykt-aisaas
# tzdata: 日志时间戳需要 Asia/Shanghai
RUN apk add --no-cache opus opusfile ca-certificates tzdata

WORKDIR /app

COPY --from=build /out/xiaozhi-server /app/xiaozhi-server
# 内置默认配置作为兜底；生产环境用挂载覆盖整个 configs 目录。
COPY configs/ /app/configs/

# 服务通过 config.Load("configs/config.yaml") 读取相对路径配置，
# 因此工作目录必须是 /app。
EXPOSE 8080

ENTRYPOINT ["/app/xiaozhi-server"]
