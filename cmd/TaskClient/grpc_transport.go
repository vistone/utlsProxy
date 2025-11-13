package main

import (
	"context"
	"fmt"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"utlsProxy/internal/taskapi"
)

// GRPCTransport gRPC传输实现
type GRPCTransport struct {
	conn   *grpc.ClientConn
	client taskapi.TaskServiceClient
	closed int32
}

// NewGRPCTransport 创建新的gRPC传输实例
func NewGRPCTransport(address string) (*GRPCTransport, error) {
	conn, err := taskapi.Dial(address)
	if err != nil {
		return nil, err
	}

	client := taskapi.NewTaskServiceClient(conn)

	return &GRPCTransport{
		conn:   conn,
		client: client,
	}, nil
}

// Execute 执行任务请求
func (t *GRPCTransport) Execute(ctx context.Context, req *taskapi.TaskRequest) (*taskapi.TaskResponse, error) {
	if atomic.LoadInt32(&t.closed) == 1 {
		return nil, fmt.Errorf("传输已关闭")
	}
	return t.client.Execute(ctx, req)
}

// IsReady 检查传输是否就绪
func (t *GRPCTransport) IsReady() bool {
	if atomic.LoadInt32(&t.closed) == 1 {
		return false
	}
	return t.conn.GetState() == connectivity.Ready
}

// Close 关闭传输连接
func (t *GRPCTransport) Close() error {
	if !atomic.CompareAndSwapInt32(&t.closed, 0, 1) {
		return nil // 已经关闭
	}
	return t.conn.Close()
}

// GetConn 获取底层连接（用于等待就绪等操作）
func (t *GRPCTransport) GetConn() *grpc.ClientConn {
	return t.conn
}

