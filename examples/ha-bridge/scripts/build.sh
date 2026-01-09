#!/bin/bash
# Open-XiaoAI HA Bridge 构建脚本
# 自动设置 CGO 环境变量

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONNX_DIR="$SCRIPT_DIR/third_party/onnxruntime"

# 检查 ONNX Runtime 是否存在
if [ ! -d "$ONNX_DIR" ]; then
    echo "❌ ONNX Runtime 未找到，正在下载..."
    mkdir -p "$ONNX_DIR"
    curl -sL https://github.com/microsoft/onnxruntime/releases/download/v1.16.3/onnxruntime-linux-x64-1.16.3.tgz | \
        tar xz -C "$ONNX_DIR" --strip-components=1
    echo "✅ ONNX Runtime 下载完成"
fi

# 检查模型是否存在
if [ ! -f "$SCRIPT_DIR/models/silero_vad.onnx" ]; then
    echo "❌ Silero VAD 模型未找到，正在下载..."
    mkdir -p "$SCRIPT_DIR/models"
    curl -sL -o "$SCRIPT_DIR/models/silero_vad.onnx" \
        https://github.com/snakers4/silero-vad/raw/master/files/silero_vad.onnx
    echo "✅ Silero VAD 模型下载完成"
fi

# 设置 CGO 环境变量
export CGO_ENABLED=1
export CGO_CFLAGS="-I$ONNX_DIR/include"
export CGO_LDFLAGS="-L$ONNX_DIR/lib"

# 解析命令
CMD="${1:-build}"

case "$CMD" in
    build)
        echo "🔨 构建中..."
        go build -o ha-bridge ./cmd/bridge
        echo "✅ 构建完成: ./ha-bridge"
        ;;
    run)
        echo "🚀 运行中..."
        export LD_LIBRARY_PATH="$ONNX_DIR/lib:$LD_LIBRARY_PATH"
        go run ./cmd/bridge "${@:2}"
        ;;
    test)
        echo "🧪 测试中..."
        go test ./...
        ;;
    clean)
        echo "🧹 清理中..."
        rm -f ha-bridge
        go clean
        echo "✅ 清理完成"
        ;;
    *)
        echo "用法: $0 {build|run|test|clean}"
        echo ""
        echo "命令:"
        echo "  build  - 构建项目"
        echo "  run    - 直接运行 (go run)"
        echo "  test   - 运行测试"
        echo "  clean  - 清理构建产物"
        exit 1
        ;;
esac
