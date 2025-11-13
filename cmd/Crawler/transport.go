package main

import "strings"

type transportKind int

const (
	transportGRPC transportKind = iota
	transportQUIC
)

type transportMetrics struct {
	requests      *int64
	success       *int64
	failed        *int64
	requestBytes  *int64
	responseBytes *int64
	duration      *int64
}

func (t transportKind) label() string {
	switch t {
	case transportGRPC:
		return "gRPC"
	case transportQUIC:
		return "QUIC"
	default:
		return "UNKNOWN"
	}
}

func (t transportKind) prefix() string {
	return strings.ToLower(t.label())
}

func (c *Crawler) metricsForTransport(t transportKind) transportMetrics {
	switch t {
	case transportGRPC:
		return transportMetrics{
			requests:      &c.stats.GRPCRequests,
			success:       &c.stats.GRPCSuccess,
			failed:        &c.stats.GRPCFailed,
			requestBytes:  &c.stats.GRPCRequestBytes,
			responseBytes: &c.stats.GRPCResponseBytes,
			duration:      &c.stats.GRPCDuration,
		}
	case transportQUIC:
		return transportMetrics{
			requests:      &c.stats.QUICRequests,
			success:       &c.stats.QUICSuccess,
			failed:        &c.stats.QUICFailed,
			requestBytes:  &c.stats.QUICRequestBytes,
			responseBytes: &c.stats.QUICResponseBytes,
			duration:      &c.stats.QUICDuration,
		}
	default:
		return transportMetrics{}
	}
}
