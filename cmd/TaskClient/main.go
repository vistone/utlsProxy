package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"utlsProxy/internal/taskapi"
)

func main() {
	const (
		// serverAddress 支持IPv4和IPv6地址
		// IPv4格式: "172.93.47.57:9091"
		// IPv6格式: "[2607:8700:5500:2943::cc67]:9091" 或 "2607:8700:5500:2943::cc67:9091"（会自动格式化）
		serverAddress   = "2607:8700:5500:2943::2:9091"
		requestPath     = "/rt/earth/BulkMetadata/pb=!1m2!1s3142!2u1003"
		defaultClientID = "1"
		repeatCount     = 50000
		concurrency     = 500
		requestTimeout  = 20 * time.Second // 增加超时时间以应对慢速IP
		rpcMaxAttempts  = 5
		rpcRetryDelay   = 50 * time.Millisecond
		outputDir       = "/Volumes/SSD/taskclient_data" // 响应体保存目录
		useKCP          = true                           // 是否使用KCP传输（需要与服务器端配置一致）
	)

	if repeatCount <= 0 {
		log.Fatal("repeatCount 必须大于 0")
	}
	if concurrency <= 0 {
		log.Fatal("concurrency 必须大于 0")
	}

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("创建输出目录失败: %v", err)
	}
	log.Printf("响应体将保存到目录: %s", outputDir)

	var conn *grpc.ClientConn
	var err error
	var client taskapi.TaskServiceClient
	var connMutex sync.Mutex

	// 建立连接的辅助函数
	establishConnection := func() (*grpc.ClientConn, error) {
		if useKCP {
			// 使用KCP传输
			kcpConfig := taskapi.DefaultKCPConfig()
			return taskapi.DialKCP(serverAddress, kcpConfig)
		} else {
			// 使用TCP传输（默认）
			return taskapi.Dial(serverAddress)
		}
	}

	// 初始连接
	conn, err = establishConnection()
	if err != nil {
		log.Fatalf("连接任务服务失败: %v", err)
	}
	log.Printf("已连接到服务器（%s传输）: %s", map[bool]string{true: "KCP", false: "TCP"}[useKCP], serverAddress)
	defer func() { _ = conn.Close() }()

	client = taskapi.NewTaskServiceClient(conn)

	// 重连函数（带重连限制和互斥锁）
	var reconnectCount int64
	var lastReconnectTime time.Time
	var isReconnecting bool
	reconnectMutex := sync.Mutex{}
	
	reconnect := func() error {
		reconnectMutex.Lock()
		defer reconnectMutex.Unlock()
		
		// 如果正在重连，等待
		if isReconnecting {
			return fmt.Errorf("正在重连中，请稍候")
		}
		
		// 限制重连频率：每3秒最多重连1次
		now := time.Now()
		if !lastReconnectTime.IsZero() && now.Sub(lastReconnectTime) < 3*time.Second {
			return fmt.Errorf("重连过于频繁，请稍后再试")
		}
		
		isReconnecting = true
		lastReconnectTime = now
		
		connMutex.Lock()
		
		// 关闭旧连接
		if conn != nil {
			_ = conn.Close()
		}
		
		// 建立新连接
		newConn, err := establishConnection()
		if err != nil {
			connMutex.Unlock()
			isReconnecting = false
			return fmt.Errorf("重连失败: %w", err)
		}
		
		conn = newConn
		client = taskapi.NewTaskServiceClient(conn)
		reconnectCount++
		
		connMutex.Unlock()
		isReconnecting = false
		
		// 只在重连次数较少时打印日志，避免日志过多
		if reconnectCount <= 3 || reconnectCount%10 == 0 {
			log.Printf("已重新连接到服务器（%s传输）: %s (重连次数: %d)", map[bool]string{true: "KCP", false: "TCP"}[useKCP], serverAddress, reconnectCount)
		}
		
		// 重连后等待一段时间，确保连接稳定
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	jobCount := repeatCount
	workerCount := concurrency
	if workerCount > jobCount {
		workerCount = jobCount
	}

	var counter uint64
	start := time.Now()

	jobs := make(chan int, jobCount)
	for i := 0; i < jobCount; i++ {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	var successCount uint64
	var failCount uint64

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for idx := range jobs {
				id := defaultClientID
				if id == "" {
					current := atomic.AddUint64(&counter, 1)
					id = fmt.Sprintf("client-%d-%d", time.Now().UnixNano(), current)
				}

				var success bool
				for attempt := 1; attempt <= rpcMaxAttempts; attempt++ {
					ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)

					// 获取当前客户端连接（可能需要加锁）
					connMutex.Lock()
					currentClient := client
					connMutex.Unlock()

					resp, err := currentClient.Execute(ctx, &taskapi.TaskRequest{
						ClientID: id,
						Path:     requestPath,
					})
					cancel()

					if err != nil {
						// 检查是否是连接错误，如果是则尝试重连
						errStr := err.Error()
						isConnectionError := strings.Contains(errStr, "closed pipe") ||
							strings.Contains(errStr, "connection error") ||
							strings.Contains(errStr, "transport is closing") ||
							strings.Contains(errStr, "connection refused") ||
							strings.Contains(errStr, "Unavailable")
						
						if isConnectionError && attempt < rpcMaxAttempts {
							// 尝试重连（只在不是最后一次尝试时重连）
							if reconnectErr := reconnect(); reconnectErr == nil {
								// 重连成功，继续重试请求（reconnect内部已经等待了200ms）
								continue // 重试请求
							} else {
								// 重连失败或被限制，等待后继续
								if attempt == rpcMaxAttempts-1 {
									log.Printf("[任务 %d] 重连失败或被限制（第 %d/%d 次）: %v", idx, attempt, rpcMaxAttempts, reconnectErr)
								}
							}
						}
						
						if attempt == rpcMaxAttempts {
							atomic.AddUint64(&failCount, 1)
							log.Printf("[任务 %d] gRPC 调用失败（第 %d/%d 次）: %v", idx, attempt, rpcMaxAttempts, err)
						}
						// 只在最后一次尝试失败时记录日志，减少日志输出
						time.Sleep(rpcRetryDelay)
						continue
					}

					if resp.ErrorMessage != "" {
						if attempt == rpcMaxAttempts {
							atomic.AddUint64(&failCount, 1)
							log.Printf("[任务 %d] 服务器返回错误（第 %d/%d 次）: %s (status=%d)", idx, attempt, rpcMaxAttempts, resp.ErrorMessage, resp.StatusCode)
						}
						// 只在最后一次尝试失败时记录日志，减少日志输出
						time.Sleep(rpcRetryDelay)
						continue
					}

					atomic.AddUint64(&successCount, 1)

					// 所有响应体都通过resp.Body传输，立即写入文件并释放内存
					var bodyLen int
					if len(resp.Body) > 0 {
						bodyLen = len(resp.Body)
						// 保存响应体到文件（gzip格式）
						filename := fmt.Sprintf("task_%d_%d_%d.gz", idx, attempt, time.Now().UnixNano())
						filePath := filepath.Join(outputDir, filename)
						if err := os.WriteFile(filePath, resp.Body, 0644); err != nil {
							// 只在保存失败时记录日志
							log.Printf("[任务 %d] 警告: 保存响应体到文件失败: %v", idx, err)
						}
						// 立即释放响应体内存，避免内存累积
						resp.Body = nil
					}

					// 采样日志：每1000次成功记录一次，减少日志输出和内存占用
					successCountValue := atomic.LoadUint64(&successCount)
					if successCountValue%1000 == 0 {
						log.Printf("[任务 %d] 成功（第 %d/%d 次）: client_id=%s status=%d body_len=%d", idx, attempt, rpcMaxAttempts, resp.ClientID, resp.StatusCode, bodyLen)
					}

					success = true
					break
				}

				if !success {
					log.Printf("[任务 %d] 所有尝试均失败", idx)
				}
			}
		}()
	}

	wg.Wait()

	elapsed := time.Since(start)
	log.Printf("任务发送完成，耗时 %v，成功 %d，失败 %d", elapsed, successCount, failCount)
}
