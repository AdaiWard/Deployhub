package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/deployhub-io/deployhub/internal/model"
)

func (w *Worker) followRuntimeLogs(parent context.Context, deploymentID, containerName, since string) {
	w.stopLogFollower(containerName)
	ctx, cancel := context.WithCancel(parent)
	w.logsMu.Lock()
	w.logs[containerName] = cancel
	w.logsMu.Unlock()
	go func() {
		defer func() {
			w.logsMu.Lock()
			delete(w.logs, containerName)
			w.logsMu.Unlock()
		}()
		cmd := exec.CommandContext(ctx, "docker", "logs", "--follow", "--timestamps", "--since", since, containerName)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			return
		}
		go w.scanRuntimeOutput(ctx, deploymentID, "runtime", stdout)
		go w.scanRuntimeOutput(ctx, deploymentID, "runtime_error", stderr)
		_ = cmd.Wait()
	}()
}

func (w *Worker) stopLogFollower(containerName string) {
	w.logsMu.Lock()
	defer w.logsMu.Unlock()
	if cancel, ok := w.logs[containerName]; ok {
		cancel()
		delete(w.logs, containerName)
	}
}

func (w *Worker) scanRuntimeOutput(ctx context.Context, deploymentID, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if firstSpace := strings.IndexByte(line, ' '); firstSpace > 0 {
			line = line[firstSpace+1:]
		}
		w.log(ctx, deploymentID, stream, line)
	}
}

func (w *Worker) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.collectMetrics(ctx)
		}
	}
}

func (w *Worker) collectMetrics(ctx context.Context) {
	rows, err := w.db.Query(ctx, `
		SELECT DISTINCT ON (project_id) project_id,container_name,finished_at
		FROM deployments WHERE status='succeeded' AND container_name<>''
		ORDER BY project_id,finished_at DESC`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var projectID, container string
		var started time.Time
		if rows.Scan(&projectID, &container, &started) != nil {
			continue
		}
		metrics, err := w.containerMetrics(ctx, projectID, container, started)
		if err == nil {
			w.publishMetrics(ctx, projectID, metrics)
		}
	}
}

func (w *Worker) containerMetrics(ctx context.Context, projectID, container string, started time.Time) (model.Metrics, error) {
	output, err := w.command(ctx, "", nil, "docker", "stats", "--no-stream", "--format", "{{json .}}", container)
	if err != nil {
		return model.Metrics{}, err
	}
	var stats struct {
		CPUPercent string `json:"CPUPerc"`
		MemUsage   string `json:"MemUsage"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &stats); err != nil {
		return model.Metrics{}, err
	}
	memoryParts := strings.Split(stats.MemUsage, "/")
	usage, limit := float64(0), float64(0)
	if len(memoryParts) > 0 {
		usage = parseBytes(strings.TrimSpace(memoryParts[0])) / 1024 / 1024
	}
	if len(memoryParts) > 1 {
		limit = parseBytes(strings.TrimSpace(memoryParts[1])) / 1024 / 1024
	}
	cpu, _ := strconv.ParseFloat(strings.TrimSuffix(stats.CPUPercent, "%"), 64)
	restartsText, _ := w.command(ctx, "", nil, "docker", "inspect", "--format", "{{.RestartCount}}", container)
	restarts, _ := strconv.Atoi(strings.TrimSpace(restartsText))
	manualRestarts, err := w.queue.Client().Get(ctx, "deployhub:restarts:"+projectID).Int()
	if err == nil && manualRestarts > restarts {
		restarts = manualRestarts
	}
	return model.Metrics{
		CPUPercent: cpu, MemoryUsageMB: usage, MemoryLimitMB: limit,
		RestartCount: restarts, UptimeSeconds: int64(time.Since(started).Seconds()), CollectedAt: time.Now().UTC(),
	}, nil
}

func parseBytes(value string) float64 {
	units := []struct {
		name       string
		multiplier float64
	}{
		{"GiB", 1024 * 1024 * 1024}, {"MiB", 1024 * 1024}, {"KiB", 1024},
		{"GB", 1e9}, {"MB", 1e6}, {"kB", 1000}, {"KB", 1000}, {"B", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.name) {
			number := strings.TrimSpace(strings.TrimSuffix(value, unit.name))
			parsed, _ := strconv.ParseFloat(number, 64)
			return parsed * unit.multiplier
		}
	}
	return 0
}
