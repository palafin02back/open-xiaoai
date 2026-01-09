# 构建阶段
FROM golang:1.22-alpine AS builder

WORKDIR /app

# 安装依赖
RUN apk add --no-cache git gcc musl-dev

# 复制预下载的 ONNX Runtime
COPY third_party/onnxruntime/lib/* /usr/lib/
COPY third_party/onnxruntime/include/* /usr/include/

# 复制 go.mod/sum
COPY go.mod go.sum* ./
RUN go mod download

# 复制源码
COPY . .

# 构建
ARG VERSION=dev
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=1 go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
    -o ha-bridge ./cmd/bridge

# 运行阶段
FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

# 复制预下载的 ONNX Runtime
COPY third_party/onnxruntime/lib/libonnxruntime.so* /usr/lib/

# 复制 Silero VAD 模型
COPY models/ /app/models/

COPY --from=builder /app/ha-bridge .
COPY configs/config.yaml ./config.yaml

# 默认端口
EXPOSE 4399

# 默认时区
ENV TZ=Asia/Shanghai
ENV LD_LIBRARY_PATH=/usr/lib

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:4399/health || exit 1

ENTRYPOINT ["./ha-bridge"]
CMD ["-config", "config.yaml"]
