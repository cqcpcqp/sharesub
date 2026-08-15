package operations

import (
	"context"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

const (
	StatusHealthy     = domain.RuntimeStatusHealthy
	StatusWarning     = domain.RuntimeStatusWarning
	StatusCritical    = domain.RuntimeStatusCritical
	StatusPending     = domain.RuntimeStatusPending
	StatusDisabled    = domain.RuntimeStatusDisabled
	StatusUnavailable = domain.RuntimeStatusUnavailable
)

const (
	cpuWarningPercent      = 80
	cpuCriticalPercent     = 95
	cpuSamplePeriod        = 5 * time.Second
	memoryWarningPercent   = 85
	memoryCriticalPercent  = 95
	goroutineWarningCount  = 8_000
	goroutineCriticalCount = 15_000
)

type DatabaseStatusProvider interface {
	AdminDatabaseRuntime(context.Context) domain.AdminRuntimeDatabase
}

type resourceReader interface {
	CPUUsageNanos() (uint64, bool)
	CPULimitCores() (float64, bool)
	MemoryBytes() (uint64, uint64, bool)
}

type Monitor struct {
	database DatabaseStatusProvider
	reader   resourceReader
	now      func() time.Time

	mu                sync.Mutex
	lastCPUUsageNanos uint64
	lastCPUSampleAt   time.Time
	cpu               domain.AdminRuntimeMetric
	jobs              map[string]domain.AdminRuntimeJob
}

func NewMonitor(database DatabaseStatusProvider) *Monitor {
	m := &Monitor{
		database: database,
		reader:   linuxCgroupReader{},
		now:      time.Now,
		cpu:      domain.AdminRuntimeMetric{Status: StatusUnavailable},
		jobs:     make(map[string]domain.AdminRuntimeJob),
	}
	m.initializeCPUSample()
	return m
}

func (m *Monitor) initializeCPUSample() {
	usage, ok := m.reader.CPUUsageNanos()
	if !ok {
		return
	}
	m.lastCPUUsageNanos = usage
	m.lastCPUSampleAt = m.now()
}

func (m *Monitor) RunCPUSampler(ctx context.Context) {
	ticker := time.NewTicker(cpuSamplePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sampleCPU(m.now())
		}
	}
}

func (m *Monitor) RegisterJob(id, name string, enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := StatusPending
	if !enabled {
		status = StatusDisabled
	}
	m.jobs[id] = domain.AdminRuntimeJob{ID: id, Name: name, Status: status}
}

func (m *Monitor) RecordJobSuccess(id, result string, duration time.Duration) {
	m.recordJob(id, "", result, duration)
}

func (m *Monitor) RecordJobFailure(id string, err error, duration time.Duration) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	m.recordJob(id, message, "", duration)
}

func (m *Monitor) RecordJobWarning(id, warning, result string, duration time.Duration) {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	job, exists := m.jobs[id]
	if !exists {
		return
	}
	job.Status = StatusWarning
	job.LastRunAt = &now
	job.LastErrorAt = &now
	job.LastError = warning
	job.LastDurationMS = duration.Milliseconds()
	job.LastResult = result
	m.jobs[id] = job
}

func (m *Monitor) recordJob(id, errorMessage, result string, duration time.Duration) {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	job, exists := m.jobs[id]
	if !exists {
		return
	}
	job.LastRunAt = &now
	job.LastDurationMS = duration.Milliseconds()
	if errorMessage == "" {
		job.Status = StatusHealthy
		job.LastSuccessAt = &now
		job.LastError = ""
		job.LastResult = result
	} else {
		job.Status = StatusWarning
		job.LastErrorAt = &now
		job.LastError = errorMessage
	}
	m.jobs[id] = job
}

func (m *Monitor) Snapshot(ctx context.Context) domain.AdminRuntimeStatus {
	now := m.now().UTC()
	database := domain.AdminRuntimeDatabase{Status: StatusUnavailable}
	if m.database != nil {
		database = m.database.AdminDatabaseRuntime(ctx)
	}
	jobs, jobsStatus := m.jobSnapshot()
	return domain.AdminRuntimeStatus{
		CollectedAt: now,
		CPU:         m.cpuSnapshot(),
		Memory:      m.memorySnapshot(),
		Database:    database,
		Goroutines:  goroutineSnapshot(),
		JobsStatus:  jobsStatus,
		Jobs:        jobs,
	}
}

func (m *Monitor) sampleCPU(now time.Time) {
	usage, usageOK := m.reader.CPUUsageNanos()
	cores, coresOK := m.reader.CPULimitCores()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !usageOK || !coresOK || cores <= 0 || m.lastCPUSampleAt.IsZero() || usage < m.lastCPUUsageNanos {
		m.cpu = domain.AdminRuntimeMetric{Status: StatusUnavailable}
		if usageOK {
			m.lastCPUUsageNanos = usage
			m.lastCPUSampleAt = now
		}
		return
	}
	elapsed := now.Sub(m.lastCPUSampleAt).Seconds()
	if elapsed <= 0 {
		m.cpu = domain.AdminRuntimeMetric{Status: StatusUnavailable}
		m.lastCPUUsageNanos = usage
		m.lastCPUSampleAt = now
		return
	}
	percent := round1(float64(usage-m.lastCPUUsageNanos) / 1e9 / (elapsed * cores) * 100)
	m.lastCPUUsageNanos = usage
	m.lastCPUSampleAt = now
	percent = math.Max(0, math.Min(100, percent))
	m.cpu = domain.AdminRuntimeMetric{Status: percentStatus(percent, cpuWarningPercent, cpuCriticalPercent), UsagePercent: percent}

}

func (m *Monitor) cpuSnapshot() domain.AdminRuntimeMetric {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cpu
}

func (m *Monitor) memorySnapshot() domain.AdminRuntimeMemory {
	used, total, ok := m.reader.MemoryBytes()
	if !ok || total == 0 {
		return domain.AdminRuntimeMemory{Status: StatusUnavailable}
	}
	percent := round1(float64(used) / float64(total) * 100)
	percent = math.Max(0, math.Min(100, percent))
	return domain.AdminRuntimeMemory{Status: percentStatus(percent, memoryWarningPercent, memoryCriticalPercent), UsedBytes: used, TotalBytes: total, UsagePercent: percent}
}

func goroutineSnapshot() domain.AdminRuntimeGoroutines {
	count := runtime.NumGoroutine()
	status := StatusHealthy
	if count >= goroutineCriticalCount {
		status = StatusCritical
	} else if count >= goroutineWarningCount {
		status = StatusWarning
	}
	return domain.AdminRuntimeGoroutines{Status: status, Count: count}
}

func (m *Monitor) jobSnapshot() ([]domain.AdminRuntimeJob, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := make([]domain.AdminRuntimeJob, 0, len(m.jobs))
	status := StatusHealthy
	for _, job := range m.jobs {
		jobs = append(jobs, job)
		if job.Status == StatusWarning || job.Status == StatusCritical {
			status = StatusWarning
		} else if job.Status == StatusPending && status == StatusHealthy {
			status = StatusPending
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	return jobs, status
}

func percentStatus(percent, warning, critical float64) string {
	if percent >= critical {
		return StatusCritical
	}
	if percent >= warning {
		return StatusWarning
	}
	return StatusHealthy
}

func round1(value float64) float64 { return math.Round(value*10) / 10 }

type linuxCgroupReader struct{}

func (linuxCgroupReader) CPUUsageNanos() (uint64, bool) {
	if raw, err := os.ReadFile("/sys/fs/cgroup/cpu.stat"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "usage_usec" {
				value, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					return value * 1000, true
				}
			}
		}
	}
	return readUintFile("/sys/fs/cgroup/cpuacct/cpuacct.usage")
}

func (linuxCgroupReader) CPULimitCores() (float64, bool) {
	if raw, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 2 && fields[0] != "max" {
			quota, quotaErr := strconv.ParseFloat(fields[0], 64)
			period, periodErr := strconv.ParseFloat(fields[1], 64)
			if quotaErr == nil && periodErr == nil && quota > 0 && period > 0 {
				return quota / period, true
			}
		}
	}
	quota, quotaOK := readIntFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	period, periodOK := readIntFile("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if quotaOK && periodOK && quota > 0 && period > 0 {
		return float64(quota) / float64(period), true
	}
	return 0, false
}

func (linuxCgroupReader) MemoryBytes() (uint64, uint64, bool) {
	if used, ok := readUintFile("/sys/fs/cgroup/memory.current"); ok {
		raw, err := os.ReadFile("/sys/fs/cgroup/memory.max")
		if err != nil || strings.TrimSpace(string(raw)) == "max" {
			return 0, 0, false
		}
		total, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		return used, total, err == nil && total > 0
	}
	used, usedOK := readUintFile("/sys/fs/cgroup/memory/memory.usage_in_bytes")
	total, totalOK := readUintFile("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	if !usedOK || !totalOK || total == 0 || total >= 1<<60 {
		return 0, 0, false
	}
	return used, total, true
}

func readUintFile(path string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	return value, err == nil
}

func readIntFile(path string) (int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return value, err == nil
}
