package magi_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
)

func TestDockerCodeRunner_RequiresImage(t *testing.T) {
	if _, err := magi.NewDockerCodeRunnerAdapter(magi.DockerCodeRunnerPolicy{}, nil); err == nil {
		t.Fatal("expected image required error")
	}
}

func TestDockerCodeRunner_RejectsBlockedRequestsBeforeExec(t *testing.T) {
	called := int32(0)
	exec := func(ctx context.Context, args ...string) ([]byte, error) {
		atomic.AddInt32(&called, 1)
		return []byte("ran"), nil
	}
	adapter, err := magi.NewDockerCodeRunnerAdapter(magi.DockerCodeRunnerPolicy{
		CodeRunnerPolicy: magi.DefaultCodeRunnerPolicy(),
		Image:            "python:3.12-slim",
	}, exec)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := adapter.Run(context.Background(), "js", "console.log(1)"); err == nil {
		t.Fatal("unsupported language must be rejected")
	}
	if _, err := adapter.Run(context.Background(), "Python", "import os; os.system('rm -rf /')"); err == nil {
		t.Fatal("blocked pattern must be rejected")
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("docker must not run for rejected requests")
	}
}

func TestDockerCodeRunner_RunsIsolatedContainer(t *testing.T) {
	var gotArgs []string
	exec := func(ctx context.Context, args ...string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return []byte("42\n"), nil
	}
	adapter, err := magi.NewDockerCodeRunnerAdapter(magi.DockerCodeRunnerPolicy{
		CodeRunnerPolicy: magi.DefaultCodeRunnerPolicy(),
		Image:            "python:3.12-slim", MemoryMB: 512, CPUs: "1.0", DockerTimeout: 5,
	}, exec)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := adapter.Run(context.Background(), "Python", "print(6*7)")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "42" {
		t.Fatalf("output = %q", out)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"run", "--rm", "--network", "none", "--pids-limit", "64", "--memory", "512m", "--cpus", "1.0", "-i", "python:3.12-slim", "python3", "-c", "print(6*7)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("docker args missing %q: %s", want, joined)
		}
	}
}

func TestDockerCodeRunner_ReportsTimeout(t *testing.T) {
	exec := func(ctx context.Context, args ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	adapter, err := magi.NewDockerCodeRunnerAdapter(magi.DockerCodeRunnerPolicy{
		CodeRunnerPolicy: magi.DefaultCodeRunnerPolicy(),
		Image:            "python:3.12-slim", DockerTimeout: 1,
	}, exec)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	start := time.Now()
	_, err = adapter.Run(context.Background(), "Python", "print(1)")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout must bound execution")
	}
}

func TestDockerCodeRunner_SurfacesStderrOnFailure(t *testing.T) {
	exec := func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte("Traceback: boom"), errors.New("exit status 1")
	}
	adapter, err := magi.NewDockerCodeRunnerAdapter(magi.DockerCodeRunnerPolicy{
		CodeRunnerPolicy: magi.DefaultCodeRunnerPolicy(),
		Image:            "python:3.12-slim",
	}, exec)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_, err = adapter.Run(context.Background(), "Python", "raise ValueError()")
	if err == nil || !strings.Contains(err.Error(), "Traceback: boom") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}
