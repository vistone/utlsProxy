package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"utlsProxy/internal/taskapi"
)

func main() {
	const (
		serverAddress   = "172.93.47.57:9091"
		requestPath     = "/rt/earth/BulkMetadata/pb=!1m2!1s3150!2u1003"
		defaultClientID = "1"
		repeatCount     = 20000
		concurrency     = 500
		requestTimeout  = 20 * time.Second // 增加超时时间以应对慢速IP
		rpcMaxAttempts  = 5
		rpcRetryDelay   = 50 * time.Millisecond
		outputDir       = "/Volumes/SSD/taskclient_data" // 响应体保存目录
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

	conn, err := taskapi.Dial(serverAddress)
	if err != nil {
		log.Fatalf("连接任务服务失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := taskapi.NewTaskServiceClient(conn)

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
					resp, err := client.Execute(ctx, &taskapi.TaskRequest{
						ClientID: id,
						Path:     requestPath,
					})
					cancel()

					if err != nil {
						if attempt == rpcMaxAttempts {
							atomic.AddUint64(&failCount, 1)
							log.Printf("[任务 %d] gRPC 调用失败（第 %d/%d 次）: %v", idx, attempt, rpcMaxAttempts, err)
						} else {
							log.Printf("[任务 %d] gRPC 调用失败（第 %d/%d 次）: %v，准备重试", idx, attempt, rpcMaxAttempts, err)
							time.Sleep(rpcRetryDelay)
						}
						continue
					}

					if resp.ErrorMessage != "" {
						if attempt == rpcMaxAttempts {
							atomic.AddUint64(&failCount, 1)
							log.Printf("[任务 %d] 服务器返回错误（第 %d/%d 次）: %s (status=%d)", idx, attempt, rpcMaxAttempts, resp.ErrorMessage, resp.StatusCode)
						} else {
							log.Printf("[任务 %d] 服务器返回错误（第 %d/%d 次）: %s (status=%d)，准备重试", idx, attempt, rpcMaxAttempts, resp.ErrorMessage, resp.StatusCode)
							time.Sleep(rpcRetryDelay)
						}
						continue
					}

					atomic.AddUint64(&successCount, 1)
					bodyLen := len(resp.Body)
					bodyPreview := ""
					if bodyLen > 0 {
						// 显示响应体的前100个字节（十六进制）
						previewLen := bodyLen
						if previewLen > 100 {
							previewLen = 100
						}
						bodyPreview = fmt.Sprintf(", body_preview=%x", resp.Body[:previewLen])
						if bodyLen > 100 {
							bodyPreview += "..."
						}

						// 保存响应体到文件（gzip格式）
						filename := fmt.Sprintf("task_%d_%d_%d.gz", idx, attempt, time.Now().UnixNano())
						filePath := filepath.Join(outputDir, filename)
						if err := os.WriteFile(filePath, resp.Body, 0644); err != nil {
							log.Printf("[任务 %d] 警告: 保存响应体到文件失败: %v", idx, err)
						} else {
							log.Printf("[任务 %d] 响应体已保存: %s (%d 字节)", idx, filePath, bodyLen)
						}
					} else {
						bodyPreview = ", body_preview=(空)"
					}
					log.Printf("[任务 %d] 成功（第 %d/%d 次）: client_id=%s status=%d body_len=%d%s", idx, attempt, rpcMaxAttempts, resp.ClientID, resp.StatusCode, bodyLen, bodyPreview)
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
