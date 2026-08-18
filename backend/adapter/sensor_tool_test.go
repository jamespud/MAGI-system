package magi_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestSensorToolExecutor_RunsRegisteredCheck(t *testing.T) {
	var gotStdin string
	runCmd := func(ctx context.Context, command string, args []string, stdin string) ([]byte, error) {
		gotStdin = stdin
		if command == "gofmt" {
			return []byte("lint ok"), nil
		}
		return []byte("check failed: 2 issues"), errors.New("exit status 1")
	}
	exec, err := magi.NewSensorToolExecutor(magi.SensorToolConfig{
		Enabled: true,
		Checks: []magi.SensorCheck{
			{Name: "lint", Command: "gofmt", Args: []string{"-l", "."}},
			{Name: "test", Command: "go", Args: []string{"test"}, Timeout: 5},
		},
	}, runCmd)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.SensorToolName, ArgumentsJSON: `{"check":"lint","input":"package main"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		OK     bool   `json:"ok"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.OK || out.Output != "lint ok" || gotStdin != "package main" {
		t.Fatalf("out=%+v stdin=%q", out, gotStdin)
	}

	res, err = exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.SensorToolName, ArgumentsJSON: `{"check":"test"}`,
	})
	if err != nil {
		t.Fatalf("execute failing check: %v", err)
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.OK || !strings.Contains(out.Output, "check failed") {
		t.Fatalf("failing check output = %+v", out)
	}
}

func TestSensorToolExecutor_RejectsUnregisteredCheck(t *testing.T) {
	called := int32(0)
	runCmd := func(ctx context.Context, command string, args []string, stdin string) ([]byte, error) {
		atomic.AddInt32(&called, 1)
		return nil, nil
	}
	exec, err := magi.NewSensorToolExecutor(magi.SensorToolConfig{
		Enabled: true, Checks: []magi.SensorCheck{{Name: "lint", Command: "gofmt"}},
	}, runCmd)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.SensorToolName, ArgumentsJSON: `{"check":"rm"}`,
	}); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected unregistered rejection, got %v", err)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("unregistered check must not execute")
	}
}

func TestSensorToolExecutor_ReportsTimeout(t *testing.T) {
	runCmd := func(ctx context.Context, command string, args []string, stdin string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	exec, err := magi.NewSensorToolExecutor(magi.SensorToolConfig{
		Enabled: true, Checks: []magi.SensorCheck{{Name: "slow", Command: "sleep", Timeout: 1}},
	}, runCmd)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.SensorToolName, ArgumentsJSON: `{"check":"slow"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Output, "timed out") || strings.Contains(res.Output, `"ok":true`) {
		t.Fatalf("timeout output = %s", res.Output)
	}
}

func TestSensorToolExecutor_RequiresChecks(t *testing.T) {
	if _, err := magi.NewSensorToolExecutor(magi.SensorToolConfig{Enabled: false}, nil); err == nil {
		t.Fatal("expected disabled error")
	}
	if _, err := magi.NewSensorToolExecutor(magi.SensorToolConfig{Enabled: true}, nil); err == nil {
		t.Fatal("expected missing checks error")
	}
	if _, err := magi.NewSensorToolExecutor(magi.SensorToolConfig{
		Enabled: true, Checks: []magi.SensorCheck{{Name: "dup", Command: "a"}, {Name: "dup", Command: "b"}},
	}, nil); err == nil || !strings.Contains(err.Error(), "duplicate check") {
		t.Fatalf("expected duplicate check error, got %v", err)
	}
}
