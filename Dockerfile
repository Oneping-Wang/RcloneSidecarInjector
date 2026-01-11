# --- 第一阶段：构建阶段 (Builder) ---
# 使用官方 Go 语言镜像作为构建环境
FROM golang:1.25-alpine AS builder

# 设置工作目录
WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

# 1. 先复制依赖文件 (利用 Docker 缓存机制加速构建)
COPY go.mod go.sum ./
# 下载依赖
RUN go mod download

# 2. 复制源代码
COPY . .

# 3. 编译 Go 程序
# CGO_ENABLED=0: 禁用 CGO，确保生成纯静态二进制文件
# GOOS=linux: 目标系统是 Linux
# -o webhook: 输出文件名为 webhook
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook cmd/webhook/main.go

# --- 第二阶段：运行阶段 (Runner) ---
# 使用极其精简的 Alpine Linux 作为基础镜像
FROM alpine:latest

# 安装基础证书 (访问 HTTPS 需要)
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# 从第一阶段把编译好的二进制文件“偷”过来
COPY --from=builder /app/webhook .

# 暴露端口 (虽然 K8s 不强制依赖这个，但写上是好习惯)
EXPOSE 8443

# 容器启动时执行的命令
CMD ["./webhook"]