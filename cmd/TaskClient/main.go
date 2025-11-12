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
	"google.golang.org/grpc/connectivity"

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

	// 等待连接就绪
	waitForReady := func(c *grpc.ClientConn, timeout time.Duration) error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		for {
			state := c.GetState()
			if state == connectivity.Ready {
				return nil
			}
			if state == connectivity.Shutdown {
				return fmt.Errorf("连接已关闭")
			}
			if !c.WaitForStateChange(ctx, state) {
				return ctx.Err()
			}
		}
	}
	
	// 等待连接就绪（KCP可能需要更长时间，最多等待10秒）
	// 如果超时，对于KCP连接，允许继续尝试（KCP连接可能不会自动变为READY）
	initialState := conn.GetState()
	log.Printf("初始连接状态: %v", initialState)
	
	if err := waitForReady(conn, 10*time.Second); err != nil {
		if useKCP {
			// KCP连接可能不会自动变为READY状态，允许继续尝试
			log.Printf("警告: 等待KCP连接就绪超时，但允许继续尝试（当前状态: %v）", conn.GetState())
		} else {
			log.Fatalf("等待连接就绪失败: %v", err)
		}
	} else {
		log.Printf("连接已就绪，状态: %v", conn.GetState())
	}

	defer func() { _ = conn.Close() }()

	client = taskapi.NewTaskServiceClient(conn)

	// 重连函数（带重连限制和互斥锁）
	var reconnectCount int64
	var lastReconnectTime time.Time
	var isReconnecting int32 // 使用原子操作
	reconnectMutex := sync.Mutex{}

	reconnect := func() error {
		// 使用原子操作检查是否正在重连
		if !atomic.CompareAndSwapInt32(&isReconnecting, 0, 1) {
			// 如果正在重连，等待一小段时间后返回错误，让调用者重试
			time.Sleep(100 * time.Millisecond)
			return fmt.Errorf("正在重连中，请稍候")
		}
		defer atomic.StoreInt32(&isReconnecting, 0)

		reconnectMutex.Lock()

		// 限制重连频率：每2秒最多重连1次
		now := time.Now()
		if !lastReconnectTime.IsZero() && now.Sub(lastReconnectTime) < 2*time.Second {
			reconnectMutex.Unlock()
			return fmt.Errorf("重连过于频繁，请稍后再试")
		}
		lastReconnectTime = now
		reconnectMutex.Unlock()

		connMutex.Lock()

		// 关闭旧连接
		if conn != nil {
			_ = conn.Close()
		}

		// 建立新连接
		newConn, err := establishConnection()
		if err != nil {
			connMutex.Unlock()
			return fmt.Errorf("重连失败: %w", err)
		}

		conn = newConn
		client = taskapi.NewTaskServiceClient(conn)
		reconnectCount++

		connMutex.Unlock()

		// 等待连接就绪（最多等待3秒）
		waitForReady := func(c *grpc.ClientConn, timeout time.Duration) error {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			for {
				state := c.GetState()
				if state == connectivity.Ready {
					return nil
				}
				if state == connectivity.Shutdown {
					return fmt.Errorf("连接已关闭")
				}
				if !c.WaitForStateChange(ctx, state) {
					return ctx.Err()
				}
			}
		}

		// 对于KCP连接，如果等待超时，允许继续（KCP可能不会自动变为READY）
		if err := waitForReady(conn, 5*time.Second); err != nil {
			if useKCP {
				log.Printf("警告: 重连后等待KCP连接就绪超时，但允许继续尝试（当前状态: %v）", conn.GetState())
			} else {
				return fmt.Errorf("等待连接就绪失败: %w", err)
			}
		}

		if reconnectCount <= 3 || reconnectCount%10 == 0 {
			log.Printf("已重新连接到服务器（%s传输）: %s (重连次数: %d, 连接状态: %v)", map[bool]string{true: "KCP", false: "TCP"}[useKCP], serverAddress, reconnectCount, conn.GetState())
		}

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

	log.Printf("启动 %d 个worker goroutine，准备处理 %d 个任务", workerCount, jobCount)
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			defer wg.Done()
			if workerID < 3 {
				log.Printf("[Worker %d] 已启动", workerID)
			}
			for idx := range jobs {
				if workerID < 3 && idx < 3 {
					log.Printf("[Worker %d] 开始处理任务 %d", workerID, idx)
				}
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
					currentConn := conn
					connMutex.Unlock()

					// 检查连接状态，如果不是 READY 则等待或重连
					// 注意：对于KCP连接，即使状态不是READY也可能可以工作
					if currentConn != nil {
						state := currentConn.GetState()
						if state != connectivity.Ready {
							if idx < 5 {
								log.Printf("[任务 %d] 连接状态不是 READY: %v，等待或重连", idx, state)
							}
							// 如果连接正在连接中，等待一小段时间
							if state == connectivity.Connecting {
								cancel() // 取消当前上下文
								// KCP连接可能需要更长时间
								waitTime := 500 * time.Millisecond
								if useKCP {
									waitTime = 1 * time.Second
								}
								time.Sleep(waitTime)
								// 再次检查状态
								connMutex.Lock()
								if conn != nil {
									newState := conn.GetState()
									if newState == connectivity.Ready {
										currentClient = client
										currentConn = conn
									} else if useKCP && newState == connectivity.Connecting {
										// KCP连接可能一直处于CONNECTING状态，允许尝试发送请求
										currentClient = client
										currentConn = conn
									}
								}
								connMutex.Unlock()
								// 重新创建上下文
								ctx, cancel = context.WithTimeout(context.Background(), requestTimeout)
							} else if state == connectivity.TransientFailure || state == connectivity.Shutdown {
								// 连接失败，尝试重连
								cancel() // 取消当前上下文
								if reconnectErr := reconnect(); reconnectErr == nil {
									connMutex.Lock()
									currentClient = client
									currentConn = conn
									connMutex.Unlock()
									// 重新创建上下文后继续循环
									ctx, cancel = context.WithTimeout(context.Background(), requestTimeout)
									// 继续循环，重新检查连接状态
									continue
								} else {
									// 重连失败，等待后继续
									time.Sleep(rpcRetryDelay)
									// 重新创建上下文
									ctx, cancel = context.WithTimeout(context.Background(), requestTimeout)
									continue
								}
							} else if useKCP && state == connectivity.Idle {
								// KCP连接可能处于Idle状态，允许尝试发送请求
								if idx < 5 {
									log.Printf("[任务 %d] KCP连接处于Idle状态，允许尝试发送请求", idx)
								}
							}
						}
					}

					// 调试日志：记录请求发送
					if idx < 5 || idx%1000 == 0 {
						log.Printf("[任务 %d] 准备发送请求（第 %d/%d 次）", idx, attempt, rpcMaxAttempts)
					}

					// 使用 goroutine 监控请求是否超时
					done := make(chan bool, 1)
					var resp *taskapi.TaskResponse
					var err error

					go func() {
						resp, err = currentClient.Execute(ctx, &taskapi.TaskRequest{
							ClientID: id,
							Path:     requestPath,
						})
						done <- true
					}()

					// 等待请求完成或超时
					select {
					case <-done:
						// 请求完成
						cancel()
					case <-ctx.Done():
						// 请求超时
						err = ctx.Err()
						cancel()
						if idx < 5 {
							log.Printf("[任务 %d] 请求超时（第 %d/%d 次）: %v", idx, attempt, rpcMaxAttempts, err)
						}
					}

					// 调试日志：记录请求结果
					if idx < 5 || idx%1000 == 0 {
						if err != nil {
							log.Printf("[任务 %d] 请求失败（第 %d/%d 次）: %v", idx, attempt, rpcMaxAttempts, err)
						} else if resp != nil {
							log.Printf("[任务 %d] 请求成功（第 %d/%d 次）: status=%d", idx, attempt, rpcMaxAttempts, resp.StatusCode)
						}
					}

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
							cancel() // 取消当前上下文
							if reconnectErr := reconnect(); reconnectErr == nil {
								// 重连成功，重新创建上下文并重试请求
								ctx, cancel = context.WithTimeout(context.Background(), requestTimeout)
								continue // 重试请求
							} else {
								// 重连失败或被限制，等待后继续
								if attempt == rpcMaxAttempts-1 {
									log.Printf("[任务 %d] 重连失败或被限制（第 %d/%d 次）: %v", idx, attempt, rpcMaxAttempts, reconnectErr)
								}
								// 重新创建上下文
								ctx, cancel = context.WithTimeout(context.Background(), requestTimeout)
							}
						}

						if attempt == rpcMaxAttempts {
							atomic.AddUint64(&failCount, 1)
							log.Printf("[任务 %d] gRPC 调用失败（第 %d/%d 次）: %v", idx, attempt, rpcMaxAttempts, err)
						}
						// 只在最后一次尝试失败时记录日志，减少日志输出
						cancel() // 确保取消上下文
						time.Sleep(rpcRetryDelay)
						continue
					}

					if resp.ErrorMessage != "" {
						if attempt == rpcMaxAttempts {
							atomic.AddUint64(&failCount, 1)
							log.Printf("[任务 %d] 服务器返回错误（第 %d/%d 次）: %s (status=%d)", idx, attempt, rpcMaxAttempts, resp.ErrorMessage, resp.StatusCode)
						}
						// 只在最后一次尝试失败时记录日志，减少日志输出
						cancel() // 确保取消上下文
						time.Sleep(rpcRetryDelay)
						continue
					}

					// 请求成功，取消上下文
					cancel()

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
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)
	log.Printf("任务发送完成，耗时 %v，成功 %d，失败 %d", elapsed, successCount, failCount)
}
