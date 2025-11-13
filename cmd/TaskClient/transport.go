package main

import (
	"context"

	"utlsProxy/internal/taskapi"
)

// Transport 定义传输层接口，支持不同的底层协议（gRPC、QUIC等）
type Transport interface {
	// Execute 执行任务请求
	Execute(ctx context.Context, req *taskapi.TaskRequest) (*taskapi.TaskResponse, error)
	// Close 关闭传输连接
	Close() error
	// IsReady 检查传输是否就绪
	IsReady() bool
}

