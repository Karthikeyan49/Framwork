//go:build integration && performance

package integration

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Boundary observations do not pause the workload, force GC/vacuum, or enter
// individual request timers. Missing platform counters are explicit.
type aggregateResources struct {
	gc       uint32
	pauseNS  uint64
	cpu      map[string]uint64
	cpuFound bool
}

func aggregateResourceSnapshot() aggregateResources {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s := aggregateResources{gc: m.NumGC, pauseNS: m.PauseTotalNs, cpu: map[string]uint64{}}
	raw, err := os.ReadFile("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		return s
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "usage_usec", "nr_periods", "nr_throttled", "throttled_usec":
			n, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				s.cpu[fields[0]] = n
			}
		}
	}
	s.cpuFound = len(s.cpu) == 4
	return s
}

func aggregateReportResources(t *testing.T, before, after aggregateResources) {
	t.Helper()
	t.Logf("aggregate_resource_boundary go_gc_cycles=%d go_gc_pause_ms=%.3f cgroup_counters_available=%t", after.gc-before.gc, float64(after.pauseNS-before.pauseNS)/1e6, before.cpuFound && after.cpuFound)
	if !before.cpuFound || !after.cpuFound {
		return
	}
	delta := map[string]uint64{}
	for key, n := range after.cpu {
		if n < before.cpu[key] {
			t.Log("aggregate_resource_boundary cgroup counter reset; deltas unavailable")
			return
		}
		delta[key] = n - before.cpu[key]
	}
	t.Logf("aggregate_resource_boundary shared_cgroup_cpu_ms=%.3f periods=%d throttled_periods=%d throttled_ms=%.3f; shared services and Go, correlation only", float64(delta["usage_usec"])/1000, delta["nr_periods"], delta["nr_throttled"], float64(delta["throttled_usec"])/1000)
}

func aggregateBackendBoundary(t *testing.T, f *iamFixture) {
	t.Helper()
	rows, err := f.operator.Query(`SELECT backend_type,COALESCE(wait_event_type,'NONE'),COALESCE(wait_event,'NONE'),count(*) FROM pg_catalog.pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND (state='active' OR backend_type='autovacuum worker') GROUP BY 1,2,3 ORDER BY 1,2,3`)
	if err != nil {
		t.Log("aggregate_backend_boundary unavailable")
		return
	}
	defer rows.Close()
	observed := 0
	for rows.Next() {
		var kind, waitType, wait string
		var count int
		if rows.Scan(&kind, &waitType, &wait, &count) != nil {
			t.Log("aggregate_backend_boundary unavailable")
			return
		}
		observed += count
		t.Logf("aggregate_backend_boundary backend_type=%s wait_type=%s wait_event=%s count=%d", kind, waitType, wait, count)
	}
	if rows.Err() != nil {
		t.Log("aggregate_backend_boundary unavailable")
		return
	}
	t.Logf("aggregate_backend_boundary observed_active=%d; boundary sample only", observed)
}
