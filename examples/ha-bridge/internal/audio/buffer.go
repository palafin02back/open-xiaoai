// Package audio 提供音频缓冲功能
package audio

import (
	"sync"
)

// Buffer 环形音频缓冲区
type Buffer struct {
	data     []byte
	capacity int
	size     int
	readPos  int
	writePos int
	mu       sync.Mutex
}

// NewBuffer 创建新的音频缓冲区
func NewBuffer(capacity int) *Buffer {
	return &Buffer{
		data:     make([]byte, capacity),
		capacity: capacity,
	}
}

// Write 写入数据到缓冲区
func (b *Buffer) Write(data []byte) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	written := 0
	for _, byte := range data {
		if b.size >= b.capacity {
			// 缓冲区满，覆盖旧数据
			b.readPos = (b.readPos + 1) % b.capacity
		} else {
			b.size++
		}
		b.data[b.writePos] = byte
		b.writePos = (b.writePos + 1) % b.capacity
		written++
	}

	return written
}

// Read 从缓冲区读取数据
func (b *Buffer) Read(n int) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	if n > b.size {
		n = b.size
	}

	result := make([]byte, n)
	for i := 0; i < n; i++ {
		result[i] = b.data[b.readPos]
		b.readPos = (b.readPos + 1) % b.capacity
	}
	b.size -= n

	return result
}

// Peek 查看缓冲区数据但不消费
func (b *Buffer) Peek(n int) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	if n > b.size {
		n = b.size
	}

	result := make([]byte, n)
	pos := b.readPos
	for i := 0; i < n; i++ {
		result[i] = b.data[pos]
		pos = (pos + 1) % b.capacity
	}

	return result
}

// Size 返回当前缓冲区大小
func (b *Buffer) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

// Capacity 返回缓冲区容量
func (b *Buffer) Capacity() int {
	return b.capacity
}

// Clear 清空缓冲区
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.size = 0
	b.readPos = 0
	b.writePos = 0
}

// IsFull 检查缓冲区是否已满
func (b *Buffer) IsFull() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size >= b.capacity
}

// IsEmpty 检查缓冲区是否为空
func (b *Buffer) IsEmpty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size == 0
}
