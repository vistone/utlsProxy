package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"utlsProxy/internal/taskapi"
)

func (c *Crawler) executeTask(ctx context.Context, transport transportKind, req *taskapi.TaskRequest, rawRequestBytes int64) (*taskapi.TaskResponse, error) {
	metrics := c.metricsForTransport(transport)
	label := "[" + transport.label() + "]"
	start := time.Now()

	if metrics.requests != nil {
		atomic.AddInt64(metrics.requests, 1)
	}

	resp := &taskapi.TaskResponse{}

	if req == nil {
		if metrics.failed != nil {
			atomic.AddInt64(metrics.failed, 1)
		}
		resp.ErrorMessage = "空请求"
		c.addTransportDuration(metrics.duration, start)
		return resp, nil
	}

	resp.ClientID = req.ClientID

	if rawRequestBytes <= 0 {
		rawRequestBytes = int64(len(req.Path) + len(req.ClientID))
	}
	if metrics.requestBytes != nil && rawRequestBytes > 0 {
		atomic.AddInt64(metrics.requestBytes, rawRequestBytes)
	}

	if strings.TrimSpace(req.Path) == "" {
		if metrics.failed != nil {
			atomic.AddInt64(metrics.failed, 1)
		}
		resp.ErrorMessage = "path 不能为空"
		c.addTransportDuration(metrics.duration, start)
		return resp, nil
	}

	acquired, acquireErr := c.acquireTaskSlot(ctx)
	if acquireErr != nil {
		if metrics.failed != nil {
			atomic.AddInt64(metrics.failed, 1)
		}
		resp.ErrorMessage = fmt.Sprintf("%s 并发受限: %v", label, acquireErr)
		c.addTransportDuration(metrics.duration, start)
		return resp, nil
	}
	if !acquired {
		if metrics.failed != nil {
			atomic.AddInt64(metrics.failed, 1)
		}
		resp.ErrorMessage = "无法获取并发资源"
		c.addTransportDuration(metrics.duration, start)
		return resp, nil
	}
	defer func() { <-c.grpcSemaphore }()

	taskStart := time.Now()
	c.recordTaskStart()

	defer func() {
		c.recordTaskCompletion(time.Since(taskStart))
		if metrics.duration != nil {
			atomic.AddInt64(metrics.duration, time.Since(start).Microseconds())
		}
	}()

	statusCode, body, err := c.handleTaskRequest(ctx, label, transport.prefix(), req.ClientID, req.Path)
	resp.StatusCode = int32(statusCode)

	bodyLen := len(body)
	defer func() {
		if body != nil && err != nil {
			body = nil
		}
	}()

	if err != nil {
		if metrics.failed != nil {
			atomic.AddInt64(metrics.failed, 1)
		}
		resp.ErrorMessage = err.Error()
		if metrics.responseBytes != nil {
			atomic.AddInt64(metrics.responseBytes, int64(len(resp.ErrorMessage)))
		}
		return resp, nil
	}

	if statusCode == 200 {
		if metrics.success != nil {
			atomic.AddInt64(metrics.success, 1)
		}
	} else if metrics.failed != nil {
		atomic.AddInt64(metrics.failed, 1)
	}

	const maxResponseBodySize = 50 * 1024 * 1024 // 50MB
	if bodyLen > maxResponseBodySize {
		log.Printf("%s 警告: 响应体过大 (%d 字节)，超过限制 (%d 字节)，将被截断", label, bodyLen, maxResponseBodySize)
		body = body[:maxResponseBodySize]
		bodyLen = maxResponseBodySize
	}

	const largeBodyThreshold = 100 * 1024 // 100KB
	if bodyLen > largeBodyThreshold {
		tempFile := filepath.Join(c.tempFileDir, fmt.Sprintf("resp_%s_%d_%d.tmp", req.ClientID, time.Now().UnixNano(), bodyLen))
		if err := os.WriteFile(tempFile, body, 0644); err != nil {
			log.Printf("%s 警告: 写入临时文件失败: %v，将使用内存传输", label, err)
			resp.Body = body
		} else {
			body = nil
			defer func(file string) {
				if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
					log.Printf("%s 警告: 删除临时文件失败: %v (文件: %s)", label, err, file)
				}
			}(tempFile)

			fileData, err := os.ReadFile(tempFile)
			if err != nil {
				log.Printf("%s 警告: 读取临时文件失败: %v，将返回错误", label, err)
				resp.ErrorMessage = fmt.Sprintf("读取临时文件失败: %v", err)
				return resp, nil
			}
			resp.Body = fileData
		}
	} else {
		resp.Body = body
		body = nil
	}

	if metrics.responseBytes != nil {
		responseSize := int64(bodyLen)
		if resp.ErrorMessage != "" {
			responseSize += int64(len(resp.ErrorMessage))
		}
		atomic.AddInt64(metrics.responseBytes, responseSize)
	}

	if bodyLen == 0 {
		log.Printf("%s 警告: 响应体为空: client_id=%s, status=%d", label, req.ClientID, statusCode)
	}

	go func(r *taskapi.TaskResponse) {
		time.Sleep(100 * time.Millisecond)
		if r != nil {
			r.Body = nil
		}
	}(resp)

	return resp, nil
}

func (c *Crawler) addTransportDuration(durationPtr *int64, start time.Time) {
	if durationPtr == nil {
		return
	}
	atomic.AddInt64(durationPtr, time.Since(start).Microseconds())
}

func (c *Crawler) acquireTaskSlot(ctx context.Context) (bool, error) {
	select {
	case c.grpcSemaphore <- struct{}{}:
		return true, nil
	default:
		waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		select {
		case c.grpcSemaphore <- struct{}{}:
			return true, nil
		case <-waitCtx.Done():
			return false, fmt.Errorf("服务器繁忙，请稍后重试（并发限制）")
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}
