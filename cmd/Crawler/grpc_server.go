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
	address := fmt.Sprintf(":%d", c.config.ServerConfig.ServerPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("监听 gRPC 端口失败: %w", err)
	}

	server := taskapi.NewServer()
	taskapi.RegisterTaskServiceServer(server, &taskService{crawler: c})

	c.grpcListener = listener
	c.grpcServer = server

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		log.Printf("任务 gRPC 服务启动，地址 %s", address)
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("任务 gRPC 服务异常退出: %v", err)
		}
	}()

	return nil
}

func (s *taskService) Execute(ctx context.Context, req *taskapi.TaskRequest) (*taskapi.TaskResponse, error) {
	grpcStart := time.Now()
	atomic.AddInt64(&s.crawler.stats.GRPCRequests, 1)
	
	// 记录gRPC请求大小（请求体大小）
	requestSize := int64(len(req.Path))
	if req.ClientID != "" {
		requestSize += int64(len(req.ClientID))
	}
	atomic.AddInt64(&s.crawler.stats.GRPCRequestBytes, requestSize)
	
	if req == nil {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
		return &taskapi.TaskResponse{ErrorMessage: "空请求"}, nil
	}
	if req.Path == "" {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
		return &taskapi.TaskResponse{
			ClientID:     req.ClientID,
			ErrorMessage: "path 不能为空",
		}, nil
	}

	// 并发控制：获取信号量，限制同时处理的请求数
	// 先尝试立即获取，如果失败则等待最多100ms
	acquired := false
	select {
	case s.crawler.grpcSemaphore <- struct{}{}:
		acquired = true
	default:
		// 信号量已满，等待一小段时间（最多100ms）
		waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		select {
		case s.crawler.grpcSemaphore <- struct{}{}:
			acquired = true
		case <-waitCtx.Done():
			// 等待超时，返回错误让客户端重试
			atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
			return &taskapi.TaskResponse{
				ClientID:     req.ClientID,
				ErrorMessage: "服务器繁忙，请稍后重试（并发限制）",
			}, nil
		case <-ctx.Done():
			// 上下文已取消，直接返回
			atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
			return &taskapi.TaskResponse{
				ClientID:     req.ClientID,
				ErrorMessage: "请求被取消（并发限制）",
			}, ctx.Err()
		}
	}
	
	if !acquired {
		// 理论上不应该到达这里，但为了安全起见
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
		return &taskapi.TaskResponse{
			ClientID:     req.ClientID,
			ErrorMessage: "无法获取并发资源",
		}, nil
	}
	
	// 成功获取信号量，处理完成后释放
	defer func() { <-s.crawler.grpcSemaphore }()

	resp := &taskapi.TaskResponse{
		ClientID: req.ClientID,
	}

	start := time.Now()
	s.crawler.recordTaskStart()
	defer func() {
		s.crawler.recordTaskCompletion(time.Since(start))
		// 记录gRPC请求总耗时
		grpcDuration := time.Since(grpcStart)
		atomic.AddInt64(&s.crawler.stats.GRPCDuration, grpcDuration.Microseconds())
	}()

	statusCode, body, err := s.crawler.handleTaskRequest(ctx, req.ClientID, req.Path)
	resp.StatusCode = int32(statusCode)
	
	if err != nil {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
		resp.ErrorMessage = err.Error()
		// 记录gRPC响应大小（错误消息）
		responseSize := int64(len(resp.ErrorMessage))
		atomic.AddInt64(&s.crawler.stats.GRPCResponseBytes, responseSize)
		return resp, nil
	}

	if statusCode == 200 {
		atomic.AddInt64(&s.crawler.stats.GRPCSuccess, 1)
	} else {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
	}

	resp.Body = body
	// 记录gRPC响应大小（响应体）
	responseSize := int64(len(resp.Body))
	if resp.ErrorMessage != "" {
		responseSize += int64(len(resp.ErrorMessage))
	}
	atomic.AddInt64(&s.crawler.stats.GRPCResponseBytes, responseSize)
	
	// 调试日志：确认响应体已正确设置
	if len(body) > 0 {
		log.Printf("[gRPC] 响应已准备: client_id=%s, status=%d, body_len=%d", req.ClientID, statusCode, len(body))
	} else {
		log.Printf("[gRPC] 警告: 响应体为空: client_id=%s, status=%d", req.ClientID, statusCode)
	}
	
	return resp, nil
}

func (c *Crawler) handleTaskRequest(ctx context.Context, clientID, path string) (int, []byte, error) {
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
		workID := fmt.Sprintf("grpc-%s-%d", clientID, attempt)

		// 使用2秒超时，快速失败让客户端重试
		resp, _, err, duration := c.performRequestAttempt(0, 0, attempt, targetIP, pathSuffix, workID, serverTimeout)
		if err != nil {
			log.Printf("[gRPC] 任务(%s) 第 %d 次请求失败 [目标IP: %s, 耗时: %v]: %v", clientID, attempt, targetIP, duration, err)
			continue
		}

		// 如果超过2秒，直接返回超时错误，让客户端重试
		if duration > serverTimeout {
			log.Printf("[gRPC] 任务(%s) 第 %d 次超时 [目标IP: %s, 耗时: %v]，返回超时让客户端重试", clientID, attempt, targetIP, duration)
			return 0, nil, fmt.Errorf("请求超时（耗时 %v，超过 %v），请客户端重试", duration, serverTimeout)
		}

		if resp.StatusCode == 200 {
			return resp.StatusCode, resp.Body, nil
		}

		return resp.StatusCode, resp.Body, fmt.Errorf("远端返回状态码 %d", resp.StatusCode)
	}

	return 0, nil, fmt.Errorf("任务执行失败：超过最大重试次数")
}
