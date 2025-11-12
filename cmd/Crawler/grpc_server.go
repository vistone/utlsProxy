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
	
	// 记录gRPC响应大小
	responseSize := int64(len(resp.Body))
	if resp.ErrorMessage != "" {
		responseSize += int64(len(resp.ErrorMessage))
	}
	atomic.AddInt64(&s.crawler.stats.GRPCResponseBytes, responseSize)
	
	if err != nil {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
		resp.ErrorMessage = err.Error()
		return resp, nil
	}

	if statusCode == 200 {
		atomic.AddInt64(&s.crawler.stats.GRPCSuccess, 1)
	} else {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
	}

	resp.Body = body
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

		resp, _, err, duration := c.performRequestAttempt(0, 0, attempt, targetIP, pathSuffix, workID, maxTaskDuration)
		if err != nil {
			log.Printf("[gRPC] 任务(%s) 第 %d 次请求失败 [目标IP: %s, 耗时: %v]: %v", clientID, attempt, targetIP, duration, err)
			continue
		}

		if duration > maxTaskDuration {
			log.Printf("[gRPC] 任务(%s) 第 %d 次超时 [目标IP: %s, 耗时: %v]", clientID, attempt, targetIP, duration)
			continue
		}

		if resp.StatusCode == 200 {
			return resp.StatusCode, resp.Body, nil
		}

		return resp.StatusCode, resp.Body, fmt.Errorf("远端返回状态码 %d", resp.StatusCode)
	}

	return 0, nil, fmt.Errorf("任务执行失败：超过最大重试次数")
}
