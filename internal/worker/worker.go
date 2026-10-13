package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/deployhub-io/deployhub/internal/config"
	"github.com/deployhub-io/deployhub/internal/model"
	"github.com/deployhub-io/deployhub/internal/queue"
	"github.com/deployhub-io/deployhub/internal/secretbox"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Worker struct {
	cfg    config.Config
	db     *pgxpool.Pool
	queue  *queue.Queue
	box    *secretbox.Box
	logger *slog.Logger
	logsMu sync.Mutex
	logs   map[string]context.CancelFunc
}

func New(cfg config.Config, db *pgxpool.Pool, jobs *queue.Queue, box *secretbox.Box, logger *slog.Logger) *Worker {
	return &Worker{cfg: cfg, db: db, queue: jobs, box: box, logger: logger, logs: make(map[string]context.CancelFunc)}
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.requeueInterrupted(ctx); err != nil {
		return fmt.Errorf("requeue interrupted jobs: %w", err)
	}
	w.recoverLogFollowers(ctx)
	go w.metricsLoop(ctx)

	var group sync.WaitGroup
	for id := 1; id <= w.cfg.WorkerConcurrency; id++ {
		group.Add(1)
		go func(workerID int) {
			defer group.Done()
			w.consume(ctx, workerID)
		}(id)
	}
	w.logger.Info("deployment worker started", "concurrency", w.cfg.WorkerConcurrency)
	<-ctx.Done()
	group.Wait()
	return nil
}

func (w *Worker) consume(ctx context.Context, workerID int) {
	for ctx.Err() == nil {
		job, payload, err := w.queue.Claim(ctx, 5*time.Second)
		if errors.Is(err, redis.Nil) || errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error("claim job", "worker", workerID, "error", err)
			time.Sleep(time.Second)
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, w.cfg.DeployTimeout)
		err = w.process(jobCtx, job)
		cancel()
		if err != nil {
			w.logger.Error("process job", "worker", workerID, "action", job.Action, "deployment_id", job.DeploymentID, "error", err)
			if job.DeploymentID != "" {
				w.failDeployment(context.Background(), job.DeploymentID, err)
			}
		}
		if ackErr := w.queue.Ack(context.Background(), payload); ackErr != nil {
			w.logger.Error("ack job", "worker", workerID, "error", ackErr)
		}
	}
}

func (w *Worker) process(ctx context.Context, job model.DeployJob) error {
	switch job.Action {
	case "deploy":
		return w.deploy(ctx, job)
	case "restart":
		return w.restart(ctx, job.ProjectID)
	default:
		return fmt.Errorf("unknown job action %q", job.Action)
	}
}

func (w *Worker) requeueInterrupted(ctx context.Context) error {
	for {
		payload, err := w.queue.Client().RPopLPush(ctx, queue.ProcessingKey, queue.PendingKey).Result()
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return err
		}
		w.logger.Warn("requeued interrupted job", "payload", payload)
	}
}

func (w *Worker) failDeployment(ctx context.Context, deploymentID string, deployErr error) {
	message := strings.TrimSpace(deployErr.Error())
	if len(message) > 4000 {
		message = message[:4000]
	}
	_, _ = w.db.Exec(ctx, `UPDATE deployments SET status='failed', error=$1, finished_at=NOW() WHERE id=$2`, message, deploymentID)
	w.log(ctx, deploymentID, "error", message)
}

func (w *Worker) log(ctx context.Context, deploymentID, stream, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if len(message) > 16000 {
		message = message[:16000]
	}
	if _, err := w.db.Exec(ctx, `INSERT INTO deployment_logs (deployment_id,stream,message) VALUES ($1,$2,$3)`, deploymentID, stream, message); err != nil {
		w.logger.Error("save deployment log", "deployment_id", deploymentID, "error", err)
	}
}

func (w *Worker) restart(ctx context.Context, projectID string) error {
	var deploymentID, containerName string
	err := w.db.QueryRow(ctx, `
		SELECT id,container_name FROM deployments
		WHERE project_id=$1 AND status='succeeded' AND container_name<>''
		ORDER BY finished_at DESC LIMIT 1`, projectID).Scan(&deploymentID, &containerName)
	if err != nil {
		return fmt.Errorf("find active container: %w", err)
	}
	w.log(ctx, deploymentID, "system", "Restart requested")
	if _, err := w.command(ctx, "", nil, "docker", "restart", containerName); err != nil {
		return err
	}
	_ = w.queue.Client().Incr(ctx, "deployhub:restarts:"+projectID).Err()
	w.log(ctx, deploymentID, "system", "Container restarted")
	return nil
}

func (w *Worker) recoverLogFollowers(ctx context.Context) {
	rows, err := w.db.Query(ctx, `
		SELECT DISTINCT ON (project_id) id,container_name
		FROM deployments WHERE status='succeeded' AND container_name<>''
		ORDER BY project_id,finished_at DESC`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var deploymentID, containerName string
		if rows.Scan(&deploymentID, &containerName) == nil && w.containerRunning(ctx, containerName) {
			w.followRuntimeLogs(ctx, deploymentID, containerName, "2s")
		}
	}
}

func (w *Worker) publishMetrics(ctx context.Context, projectID string, metrics model.Metrics) {
	payload, err := json.Marshal(metrics)
	if err == nil {
		_ = w.queue.Client().Set(ctx, "deployhub:metrics:"+projectID, payload, 20*time.Second).Err()
	}
}
