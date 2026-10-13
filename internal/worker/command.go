package worker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

func (w *Worker) command(ctx context.Context, directory string, extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), extraEnv...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s: %w: %s", name, err, text)
		}
		return text, fmt.Errorf("%s: %w", name, err)
	}
	return text, nil
}

func (w *Worker) commandLogged(ctx context.Context, deploymentID, directory string, extraEnv []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), extraEnv...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	var group sync.WaitGroup
	group.Add(2)
	go func() { defer group.Done(); w.scanOutput(ctx, deploymentID, "stdout", stdout) }()
	go func() { defer group.Done(); w.scanOutput(ctx, deploymentID, "stderr", stderr) }()
	err = cmd.Wait()
	group.Wait()
	if err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func (w *Worker) scanOutput(ctx context.Context, deploymentID, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		w.log(ctx, deploymentID, stream, scanner.Text())
	}
}
