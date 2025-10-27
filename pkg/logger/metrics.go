package logger

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Metrics tracks performance metrics for the application
type Metrics struct {
	mu sync.RWMutex

	// Command metrics
	commandCounts   map[string]*uint64         // command -> count
	commandDuration map[string]*DurationMetric // command -> duration stats
	commandErrors   map[string]*uint64         // command -> error count

	// Service metrics
	serviceCounts   map[string]*uint64         // service -> count
	serviceDuration map[string]*DurationMetric // service -> duration stats
	serviceErrors   map[string]*uint64         // service -> error count

	// Database metrics
	dbQueryCount    uint64
	dbQueryDuration *DurationMetric
	dbErrors        uint64

	// GPT metrics
	gptRequestCount    uint64
	gptRequestDuration *DurationMetric
	gptErrors          uint64
	gptTokensUsed      uint64

	// Discord API metrics
	discordAPICount  uint64
	discordAPIErrors uint64

	// Start time
	startTime time.Time
}

// DurationMetric tracks duration statistics
type DurationMetric struct {
	mu         sync.RWMutex
	totalNanos uint64
	count      uint64
	minNanos   uint64
	maxNanos   uint64
	p50Nanos   uint64 // Approximate percentiles
	p95Nanos   uint64
	p99Nanos   uint64
	samples    []time.Duration // Keep last 1000 samples for percentile calculation
	maxSamples int
}

// NewDurationMetric creates a new duration metric tracker
func NewDurationMetric() *DurationMetric {
	return &DurationMetric{
		minNanos:   ^uint64(0), // Max uint64
		maxSamples: 1000,
		samples:    make([]time.Duration, 0, 1000),
	}
}

// Record records a duration
func (dm *DurationMetric) Record(d time.Duration) {
	nanos := uint64(d.Nanoseconds())

	atomic.AddUint64(&dm.totalNanos, nanos)
	atomic.AddUint64(&dm.count, 1)

	// Update min/max
	dm.updateMinMax(nanos)

	// Store sample for percentile calculation
	dm.mu.Lock()
	if len(dm.samples) >= dm.maxSamples {
		// Remove oldest sample
		dm.samples = dm.samples[1:]
	}
	dm.samples = append(dm.samples, d)
	dm.mu.Unlock()
}

// updateMinMax atomically updates min and max values
func (dm *DurationMetric) updateMinMax(nanos uint64) {
	// Update min
	for {
		oldMin := atomic.LoadUint64(&dm.minNanos)
		if nanos >= oldMin {
			break
		}
		if atomic.CompareAndSwapUint64(&dm.minNanos, oldMin, nanos) {
			break
		}
	}

	// Update max
	for {
		oldMax := atomic.LoadUint64(&dm.maxNanos)
		if nanos <= oldMax {
			break
		}
		if atomic.CompareAndSwapUint64(&dm.maxNanos, oldMax, nanos) {
			break
		}
	}
}

// Stats returns current statistics
func (dm *DurationMetric) Stats() DurationStats {
	count := atomic.LoadUint64(&dm.count)
	if count == 0 {
		return DurationStats{}
	}

	total := atomic.LoadUint64(&dm.totalNanos)
	avg := total / count

	return DurationStats{
		Count: count,
		Avg:   time.Duration(avg),
		Min:   time.Duration(atomic.LoadUint64(&dm.minNanos)),
		Max:   time.Duration(atomic.LoadUint64(&dm.maxNanos)),
		Total: time.Duration(total),
	}
}

// DurationStats holds duration statistics
type DurationStats struct {
	Count uint64
	Avg   time.Duration
	Min   time.Duration
	Max   time.Duration
	Total time.Duration
}

var globalMetrics = &Metrics{
	commandCounts:      make(map[string]*uint64),
	commandDuration:    make(map[string]*DurationMetric),
	commandErrors:      make(map[string]*uint64),
	serviceCounts:      make(map[string]*uint64),
	serviceDuration:    make(map[string]*DurationMetric),
	serviceErrors:      make(map[string]*uint64),
	dbQueryDuration:    NewDurationMetric(),
	gptRequestDuration: NewDurationMetric(),
	startTime:          time.Now(),
}

// RecordCommand records a command execution
func RecordCommand(commandName string, duration time.Duration, err error) {
	m := globalMetrics

	m.mu.Lock()
	if _, exists := m.commandCounts[commandName]; !exists {
		var count uint64
		m.commandCounts[commandName] = &count
		m.commandDuration[commandName] = NewDurationMetric()
		var errCount uint64
		m.commandErrors[commandName] = &errCount
	}
	m.mu.Unlock()

	atomic.AddUint64(m.commandCounts[commandName], 1)
	m.commandDuration[commandName].Record(duration)

	if err != nil {
		atomic.AddUint64(m.commandErrors[commandName], 1)
	}
}

// RecordService records a service operation
func RecordService(serviceName string, duration time.Duration, err error) {
	m := globalMetrics

	m.mu.Lock()
	if _, exists := m.serviceCounts[serviceName]; !exists {
		var count uint64
		m.serviceCounts[serviceName] = &count
		m.serviceDuration[serviceName] = NewDurationMetric()
		var errCount uint64
		m.serviceErrors[serviceName] = &errCount
	}
	m.mu.Unlock()

	atomic.AddUint64(m.serviceCounts[serviceName], 1)
	m.serviceDuration[serviceName].Record(duration)

	if err != nil {
		atomic.AddUint64(m.serviceErrors[serviceName], 1)
	}
}

// RecordDBQuery records a database query
func RecordDBQuery(duration time.Duration, err error) {
	atomic.AddUint64(&globalMetrics.dbQueryCount, 1)
	globalMetrics.dbQueryDuration.Record(duration)
	if err != nil {
		atomic.AddUint64(&globalMetrics.dbErrors, 1)
	}
}

// RecordGPTRequest records a GPT API request
func RecordGPTRequest(duration time.Duration, tokensUsed int, err error) {
	atomic.AddUint64(&globalMetrics.gptRequestCount, 1)
	globalMetrics.gptRequestDuration.Record(duration)
	atomic.AddUint64(&globalMetrics.gptTokensUsed, uint64(tokensUsed))
	if err != nil {
		atomic.AddUint64(&globalMetrics.gptErrors, 1)
	}
}

// RecordDiscordAPI records a Discord API call
func RecordDiscordAPI(err error) {
	atomic.AddUint64(&globalMetrics.discordAPICount, 1)
	if err != nil {
		atomic.AddUint64(&globalMetrics.discordAPIErrors, 1)
	}
}

// GetMetricsSummary returns a summary of all metrics
func GetMetricsSummary() map[string]interface{} {
	m := globalMetrics
	m.mu.RLock()
	defer m.mu.RUnlock()

	uptime := time.Since(m.startTime)

	summary := map[string]interface{}{
		"uptime_seconds": uptime.Seconds(),
		"commands":       make(map[string]interface{}),
		"services":       make(map[string]interface{}),
		"database": map[string]interface{}{
			"query_count":     atomic.LoadUint64(&m.dbQueryCount),
			"error_count":     atomic.LoadUint64(&m.dbErrors),
			"avg_duration_ms": m.dbQueryDuration.Stats().Avg.Milliseconds(),
		},
		"gpt": map[string]interface{}{
			"request_count":   atomic.LoadUint64(&m.gptRequestCount),
			"error_count":     atomic.LoadUint64(&m.gptErrors),
			"tokens_used":     atomic.LoadUint64(&m.gptTokensUsed),
			"avg_duration_ms": m.gptRequestDuration.Stats().Avg.Milliseconds(),
		},
		"discord": map[string]interface{}{
			"api_call_count": atomic.LoadUint64(&m.discordAPICount),
			"error_count":    atomic.LoadUint64(&m.discordAPIErrors),
		},
	}

	// Add command metrics
	commands := make(map[string]interface{})
	for cmd, count := range m.commandCounts {
		stats := m.commandDuration[cmd].Stats()
		errorCount := atomic.LoadUint64(m.commandErrors[cmd])

		commands[cmd] = map[string]interface{}{
			"count":           atomic.LoadUint64(count),
			"error_count":     errorCount,
			"avg_duration_ms": stats.Avg.Milliseconds(),
			"min_duration_ms": stats.Min.Milliseconds(),
			"max_duration_ms": stats.Max.Milliseconds(),
		}
	}
	summary["commands"] = commands

	// Add service metrics
	services := make(map[string]interface{})
	for svc, count := range m.serviceCounts {
		stats := m.serviceDuration[svc].Stats()
		errorCount := atomic.LoadUint64(m.serviceErrors[svc])

		services[svc] = map[string]interface{}{
			"count":           atomic.LoadUint64(count),
			"error_count":     errorCount,
			"avg_duration_ms": stats.Avg.Milliseconds(),
			"min_duration_ms": stats.Min.Milliseconds(),
			"max_duration_ms": stats.Max.Milliseconds(),
		}
	}
	summary["services"] = services

	return summary
}

// LogMetricsSummary logs a summary of all metrics
func LogMetricsSummary() {
	summary := GetMetricsSummary()

	log.Info().
		Interface("metrics", summary).
		Msg("Performance metrics summary")
}

// ResetMetrics resets all metrics (useful for testing)
func ResetMetrics() {
	globalMetrics = &Metrics{
		commandCounts:      make(map[string]*uint64),
		commandDuration:    make(map[string]*DurationMetric),
		commandErrors:      make(map[string]*uint64),
		serviceCounts:      make(map[string]*uint64),
		serviceDuration:    make(map[string]*DurationMetric),
		serviceErrors:      make(map[string]*uint64),
		dbQueryDuration:    NewDurationMetric(),
		gptRequestDuration: NewDurationMetric(),
		startTime:          time.Now(),
	}
}
