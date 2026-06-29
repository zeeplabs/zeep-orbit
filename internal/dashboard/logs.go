package dashboard

import (
	"strings"
	"sync"
	"time"
)

const maxBodyCapture = 2048

var ignoredPrefixes = []string{
	"/api/logs",
	"/dashboard/api/logs",
}

type LogEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	App         string    `json:"app"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	Query       string    `json:"query,omitempty"`
	Status      int       `json:"status"`
	LatencyMs   int64     `json:"latency_ms"`
	UserAgent   string    `json:"user_agent,omitempty"`
	RemoteAddr  string    `json:"remote_addr,omitempty"`
	ReqBody     string    `json:"req_body,omitempty"`
	ResBody     string    `json:"res_body,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
}

type LogMetrics struct {
	TotalRequests   int                  `json:"total_requests"`
	RequestsPerApp  map[string]int       `json:"requests_per_app"`
	AvgLatencyMs    int64                `json:"avg_latency_ms"`
	Errors4xx       int                  `json:"errors_4xx"`
	Errors5xx       int                  `json:"errors_5xx"`
	MethodBreakdown map[string]int       `json:"method_breakdown"`
}

type RingBuffer struct {
	mu    sync.RWMutex
	entries []LogEntry
	capacity int
	pos      int
	count    int
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		entries:  make([]LogEntry, capacity),
		capacity: capacity,
	}
}

func (rb *RingBuffer) Push(e LogEntry) {
	for _, prefix := range ignoredPrefixes {
		if strings.HasPrefix(e.Path, prefix) {
			return
		}
	}
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.entries[rb.pos] = e
	rb.pos = (rb.pos + 1) % rb.capacity
	if rb.count < rb.capacity {
		rb.count++
	}
}

func (rb *RingBuffer) Recent(n int, appFilter string, allowedApps map[string]bool) []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n <= 0 || n > rb.count {
		n = rb.count
	}

	result := make([]LogEntry, 0, n)
	start := rb.pos - 1
	if start < 0 {
		start = rb.capacity - 1
	}

	for i := 0; i < rb.count && len(result) < n; i++ {
		idx := (start - i + rb.capacity) % rb.capacity
		e := rb.entries[idx]
		if appFilter != "" && e.App != appFilter {
			continue
		}
		if allowedApps != nil && !allowedApps[e.App] {
			continue
		}
		result = append(result, e)
	}

	return result
}

func (rb *RingBuffer) Metrics(allowedApps map[string]bool) LogMetrics {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	m := LogMetrics{
		RequestsPerApp:  make(map[string]int),
		MethodBreakdown: make(map[string]int),
	}

	var totalLatency int64
	window := time.Now().Add(-1 * time.Minute)

	for i := 0; i < rb.count; i++ {
		e := rb.entries[i]
		if e.Timestamp.Before(window) {
			continue
		}
		if allowedApps != nil && !allowedApps[e.App] {
			continue
		}

		m.TotalRequests++
		m.RequestsPerApp[e.App]++
		totalLatency += e.LatencyMs
		m.MethodBreakdown[e.Method]++

		if e.Status >= 400 && e.Status < 500 {
			m.Errors4xx++
		} else if e.Status >= 500 {
			m.Errors5xx++
		}
	}

	if m.TotalRequests > 0 {
		m.AvgLatencyMs = totalLatency / int64(m.TotalRequests)
	}

	return m
}

func ExtractApp(path string) string {
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
