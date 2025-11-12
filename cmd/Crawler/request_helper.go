package main

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"utlsProxy/src"
)

func (c *Crawler) performRequestAttempt(workerID, taskID, attempt int, targetIP, pathSuffix, workID string, timeout time.Duration) (*src.UTlsResponse, string, error, time.Duration) {
	formattedIP := targetIP
	if ip := net.ParseIP(targetIP); ip != nil && ip.To4() == nil {
		formattedIP = "[" + targetIP + "]"
	}
	fullPath := fmt.Sprintf("https://%s%s", formattedIP, pathSuffix)

	if timeout <= 0 {
		timeout = maxTaskDuration
	}

	req := &src.UTlsRequest{
		WorkID:      workID,
		Domain:      c.config.RockTreeDataConfig.HostName,
		Method:      "GET",
		Path:        fullPath,
		Headers:     c.requestHeaders,
		DomainIP:    targetIP,
		Fingerprint: c.fingerprint,
		StartTime:   time.Now(),
		Timeout:     timeout,
	}

	resp, err := c.client.Do(req)
	duration := time.Since(req.StartTime)

	if err != nil {
		atomic.AddInt64(&c.stats.FailedRequests, 1)
		atomic.AddInt64(&c.stats.TotalRequests, 1)
		atomic.AddInt64(&c.stats.TotalDuration, duration.Microseconds())
		c.recordSlowIP(targetIP, duration)
		return nil, "", err, duration
	}

	atomic.AddInt64(&c.stats.TotalRequests, 1)
	atomic.AddInt64(&c.stats.TotalDuration, duration.Microseconds())
	if resp.StatusCode == 200 {
		atomic.AddInt64(&c.stats.SuccessRequests, 1)
		atomic.AddInt64(&c.stats.TotalBytes, int64(len(resp.Body)))
	} else {
		atomic.AddInt64(&c.stats.FailedRequests, 1)
	}
	c.recordSlowIP(targetIP, duration)

	// 注意：resp.Body会在调用者使用完后立即释放
	// 调用者负责在复制body后释放resp.Body
	return resp, resp.LocalIP, nil, duration
}
