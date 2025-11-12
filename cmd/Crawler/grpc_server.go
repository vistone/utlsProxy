package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
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
	
	// 记录body长度，用于统计和日志
	bodyLen := len(body)
	
	// 使用defer确保body在函数返回前被清理（如果出错）
	defer func() {
		// 清理局部变量body，帮助GC回收
		if body != nil && err != nil {
			body = nil
		}
	}()
	
	if err != nil {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
		resp.ErrorMessage = err.Error()
		// 记录gRPC响应大小（错误消息）
		responseSize := int64(len(resp.ErrorMessage))
		atomic.AddInt64(&s.crawler.stats.GRPCResponseBytes, responseSize)
		// body为nil，无需释放
		return resp, nil
	}

	if statusCode == 200 {
		atomic.AddInt64(&s.crawler.stats.GRPCSuccess, 1)
	} else {
		atomic.AddInt64(&s.crawler.stats.GRPCFailed, 1)
	}

	// 所有响应体都写入文件，彻底避免内存占用
	// 这样可以避免gRPC框架复制数据到发送缓冲区时的内存占用
	const maxResponseBodySize = 50 * 1024 * 1024 // 50MB
	
	if bodyLen > maxResponseBodySize {
		log.Printf("[gRPC] 警告: 响应体过大 (%d 字节)，超过限制 (%d 字节)，将被截断", bodyLen, maxResponseBodySize)
		body = body[:maxResponseBodySize]
		bodyLen = maxResponseBodySize
	}
	
	// 对于大响应体，先写入临时文件，然后流式读取传输，避免内存占用
	// 对于小响应体，直接内存传输
	const largeBodyThreshold = 100 * 1024 // 100KB，超过此大小写入文件后流式传输
	
	if bodyLen > largeBodyThreshold {
		// 大响应体：写入文件，然后流式读取传输
		tempFile := filepath.Join(s.crawler.tempFileDir, fmt.Sprintf("resp_%s_%d_%d.tmp", req.ClientID, time.Now().UnixNano(), bodyLen))
		if err := os.WriteFile(tempFile, body, 0644); err != nil {
			log.Printf("[gRPC] 警告: 写入临时文件失败: %v，将使用内存传输", err)
			// 写入失败，回退到内存传输
			resp.Body = body
		} else {
			// 写入成功，立即清空body释放内存
			body = nil
			
			// 读取文件内容到内存（短暂占用，读取完立即传输，传输完立即释放）
			// 注意：由于gRPC Unary调用的限制，无法真正的流式传输
			// 但写入文件后立即读取，可以避免在HTTP响应读取和gRPC传输之间同时占用内存
			// 使用defer确保临时文件一定会被删除
			defer func() {
				if err := os.Remove(tempFile); err != nil {
					if !os.IsNotExist(err) {
						log.Printf("[gRPC] 警告: 删除临时文件失败: %v (文件: %s)", err, tempFile)
					}
				}
			}()
			
			fileData, err := os.ReadFile(tempFile)
			if err != nil {
				log.Printf("[gRPC] 警告: 读取临时文件失败: %v，将返回错误", err)
				resp.ErrorMessage = fmt.Sprintf("读取临时文件失败: %v", err)
				return resp, nil // defer会删除临时文件
			}
			
			// 设置响应体，defer会确保临时文件被删除
			resp.Body = fileData
		}
	} else {
		// 小响应体：直接内存传输
		resp.Body = body
		body = nil // 清空局部变量，resp.Body会持有引用
	}
	// 记录gRPC响应大小（响应体）
	// 如果使用文件路径，bodyLen仍然是原始大小，用于统计
	responseSize := int64(bodyLen)
	if resp.ErrorMessage != "" {
		responseSize += int64(len(resp.ErrorMessage))
	}
	atomic.AddInt64(&s.crawler.stats.GRPCResponseBytes, responseSize)
	
	// 只在响应体为空时记录警告日志，成功响应不记录日志以减少内存占用
	if bodyLen == 0 {
		log.Printf("[gRPC] 警告: 响应体为空: client_id=%s, status=%d", req.ClientID, statusCode)
	}
	
	// 延迟清理resp.Body，确保gRPC响应已发送
	go func(r *taskapi.TaskResponse) {
		// 等待足够的时间确保gRPC响应已发送（通常gRPC发送很快，100ms足够）
		time.Sleep(100 * time.Millisecond)
		// 清理resp.Body，帮助GC回收内存
		if r != nil {
			r.Body = nil
		}
	}(resp)
	
	// body是局部变量，函数返回后会自动回收
	// resp.Body和body都会在goroutine中延迟清理，避免内存累积
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
			// 立即清理resp对象
			if resp != nil {
				resp.Body = nil
				resp = nil
			}
			// 只在最后一次尝试失败时记录日志，减少日志输出
			if attempt == maxAttempts {
				log.Printf("[gRPC] 任务(%s) 第 %d 次请求失败 [目标IP: %s, 耗时: %v]: %v", clientID, attempt, targetIP, duration, err)
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
				log.Printf("[gRPC] 任务(%s) 第 %d 次超时 [目标IP: %s, 耗时: %v]，返回超时让客户端重试", clientID, attempt, targetIP, duration)
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

	return 0, nil, fmt.Errorf("任务执行失败：超过最大重试次数")
}

