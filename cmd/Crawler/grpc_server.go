package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"utlsProxy/internal/taskapi"
)

type taskService struct {
	crawler *Crawler
}

func (c *Crawler) startGRPCServer() error {
	if c.config == nil {
		return fmt.Errorf("配置未初始化: config 为 nil")
	}
	if c.config.ServerConfig.ServerPort == 0 {
		return fmt.Errorf("服务器端口未配置: ServerPort 为 0")
	}

	address := fmt.Sprintf(":%d", c.config.ServerConfig.ServerPort)

	// 使用TCP传输
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("监听 gRPC TCP 端口失败: %w", err)
	}
	if listener == nil {
		return fmt.Errorf("创建 gRPC TCP 监听器失败: listener 为 nil")
	}

	server := taskapi.NewServer()
	if server == nil {
		return fmt.Errorf("创建 gRPC TCP 服务器失败: server 为 nil")
	}
	taskapi.RegisterTaskServiceServer(server, &taskService{crawler: c})
	log.Printf("任务 gRPC 服务启动（TCP传输），地址 %s", address)

	c.grpcListener = listener
	c.grpcServer = server

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("任务 gRPC 服务异常退出: %v", err)
		}
	}()

	return nil
}

func (s *taskService) Execute(ctx context.Context, req *taskapi.TaskRequest) (*taskapi.TaskResponse, error) {
	var rawBytes int64
	if req != nil {
		rawBytes = int64(len(req.Path) + len(req.ClientID))
	}
	return s.crawler.executeTask(ctx, transportGRPC, req, rawBytes)
}

func (c *Crawler) handleTaskRequest(ctx context.Context, transportLabel string, transportPrefix string, clientID, path string) (int, []byte, error) {
	allowedIPs := c.ipAccessControl.GetAllowedIPs()
	if len(allowedIPs) == 0 {
		return 0, nil, fmt.Errorf("白名单为空，无法调度任务")
	}

	pathSuffix := path
	if pathSuffix == "" {
		return 0, nil, fmt.Errorf("path 不能为空")
	}
	if pathSuffix[0] != '/' {
		pathSuffix = "/" + pathSuffix
	}

	// 服务器端快速超时：2秒，超过2秒直接返回让客户端重试
	const serverTimeout = 2 * time.Second
	const maxAttempts = 5

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		default:
		}

		index := int(atomic.AddUint64(&c.ipSelector, 1) % uint64(len(allowedIPs)))
		targetIP := allowedIPs[index]
		workID := fmt.Sprintf("%s-%s-%d", transportPrefix, clientID, attempt)

		// 使用2秒超时，快速失败让客户端重试
		resp, _, err, duration := c.performRequestAttempt(0, 0, attempt, targetIP, pathSuffix, workID, serverTimeout)

		if err != nil {
			// 立即清理resp对象
			if resp != nil {
				resp.Body = nil
				resp = nil
			}
			// 只在最后一次尝试失败时记录日志，减少日志输出
			if attempt == maxAttempts {
				log.Printf("%s 任务(%s) 第 %d 次请求失败 [目标IP: %s, 耗时: %v]: %v", transportLabel, clientID, attempt, targetIP, duration, err)
			}
			continue
		}

		// 如果超过2秒，直接返回超时错误，让客户端重试
		if duration > serverTimeout {
			// 立即释放响应体内存
			if resp != nil {
				resp.Body = nil
				resp = nil // 清空resp对象引用，帮助GC回收
			}
			// 只在最后一次尝试超时时记录日志，减少日志输出
			if attempt == maxAttempts {
				log.Printf("%s 任务(%s) 第 %d 次超时 [目标IP: %s, 耗时: %v]，返回超时让客户端重试", transportLabel, clientID, attempt, targetIP, duration)
			}
			return 0, nil, fmt.Errorf("请求超时（耗时 %v，超过 %v），请客户端重试", duration, serverTimeout)
		}

		// 直接返回body引用，避免不必要的复制
		// 注意：调用者会立即将body写入文件，所以这里不需要复制
		statusCode := 0
		var body []byte
		if resp != nil {
			statusCode = resp.StatusCode
			// 先保存body引用
			body = resp.Body
			// 然后立即释放resp对象引用，帮助GC回收
			// body引用已保存，可以安全清空resp.Body
			resp.Body = nil
			resp = nil
		}

		if statusCode == 200 {
			return statusCode, body, nil
		}

		return statusCode, body, fmt.Errorf("远端返回状态码 %d", statusCode)
	}

	log.Printf("%s 任务(%s) 所有尝试均失败（最大尝试次数: %d）", transportLabel, clientID, maxAttempts)
	return 0, nil, fmt.Errorf("任务执行失败：超过最大重试次数")
}
