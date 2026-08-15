package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type fakeDatabaseStatus struct{ status domain.AdminRuntimeDatabase }

func (f fakeDatabaseStatus) AdminDatabaseRuntime(context.Context) domain.AdminRuntimeDatabase {
	return f.status
}

type fakeResourceReader struct {
	cpuUsage uint64
	cpuCores float64
	memUsed  uint64
	memTotal uint64
}

func (f *fakeResourceReader) CPUUsageNanos() (uint64, bool)  { return f.cpuUsage, true }
func (f *fakeResourceReader) CPULimitCores() (float64, bool) { return f.cpuCores, true }
func (f *fakeResourceReader) MemoryBytes() (uint64, uint64, bool) {
	return f.memUsed, f.memTotal, true
}

func TestMonitorSnapshotCollectsResourcesAndJobs(t *testing.T) {
	reader := &fakeResourceReader{cpuUsage: 1_000_000_000, cpuCores: 2, memUsed: 900, memTotal: 1000}
	now := time.Date(2026, 8, 15, 3, 0, 0, 0, time.UTC)
	monitor := &Monitor{
		database: fakeDatabaseStatus{status: domain.AdminRuntimeDatabase{Status: StatusHealthy, OpenConnections: 2, MaxConnections: 10}},
		reader:   reader,
		now:      func() time.Time { return now },
		jobs:     make(map[string]domain.AdminRuntimeJob),
	}
	monitor.initializeCPUSample()
	monitor.RegisterJob("cleanup", "资源清理", true)

	now = now.Add(10 * time.Second)
	reader.cpuUsage += 12_000_000_000
	monitor.sampleCPU(now)
	monitor.RecordJobSuccess("cleanup", "清理完成", 25*time.Millisecond)
	snapshot := monitor.Snapshot(context.Background())

	if snapshot.CPU.Status != StatusHealthy || snapshot.CPU.UsagePercent != 60 {
		t.Fatalf("CPU = %+v", snapshot.CPU)
	}
	if snapshot.Memory.Status != StatusWarning || snapshot.Memory.UsagePercent != 90 {
		t.Fatalf("memory = %+v", snapshot.Memory)
	}
	if snapshot.Database.OpenConnections != 2 || snapshot.Database.MaxConnections != 10 {
		t.Fatalf("database = %+v", snapshot.Database)
	}
	if snapshot.JobsStatus != StatusHealthy || len(snapshot.Jobs) != 1 || snapshot.Jobs[0].LastResult != "清理完成" {
		t.Fatalf("jobs = %s %+v", snapshot.JobsStatus, snapshot.Jobs)
	}

	now = now.Add(time.Hour)
	reader.cpuUsage += 100_000_000_000
	secondSnapshot := monitor.Snapshot(context.Background())
	if secondSnapshot.CPU != snapshot.CPU {
		t.Fatalf("CPU changed between sampler ticks: first = %+v, second = %+v", snapshot.CPU, secondSnapshot.CPU)
	}
}

func TestMonitorCPUUnavailableUntilFirstCompletedSample(t *testing.T) {
	reader := &fakeResourceReader{cpuUsage: 1_000_000_000, cpuCores: 2}
	now := time.Date(2026, 8, 15, 3, 0, 0, 0, time.UTC)
	monitor := &Monitor{
		reader: reader,
		now:    func() time.Time { return now },
		cpu:    domain.AdminRuntimeMetric{Status: StatusUnavailable},
		jobs:   make(map[string]domain.AdminRuntimeJob),
	}
	monitor.initializeCPUSample()

	if cpu := monitor.Snapshot(context.Background()).CPU; cpu.Status != StatusUnavailable {
		t.Fatalf("CPU before completed sample = %+v", cpu)
	}
}

func TestMonitorReportsFailedAndDisabledJobs(t *testing.T) {
	now := time.Date(2026, 8, 15, 3, 0, 0, 0, time.UTC)
	monitor := &Monitor{reader: &fakeResourceReader{}, now: func() time.Time { return now }, jobs: make(map[string]domain.AdminRuntimeJob)}
	monitor.RegisterJob("enabled", "启用任务", true)
	monitor.RegisterJob("disabled", "停用任务", false)
	monitor.RecordJobFailure("enabled", errors.New("temporary failure"), time.Second)

	jobs, status := monitor.jobSnapshot()
	if status != StatusWarning || len(jobs) != 2 {
		t.Fatalf("status = %q, jobs = %+v", status, jobs)
	}
	if jobs[0].Status != StatusDisabled || jobs[1].Status != StatusWarning || jobs[1].LastError != "temporary failure" {
		t.Fatalf("jobs = %+v", jobs)
	}
}
