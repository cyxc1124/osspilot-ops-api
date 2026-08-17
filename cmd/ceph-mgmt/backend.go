package main

import "context"

type Backend interface {
	Health(ctx context.Context) (any, error)
	Info(ctx context.Context) (any, error)
	Instances(ctx context.Context) (any, error)
	Stats(ctx context.Context) (any, error)
	Restart(ctx context.Context, instanceID string) (any, error)
	RollingRestart(ctx context.Context, waitSeconds int) (any, error)
}

type Mock struct{}

func (Mock) Health(context.Context) (any, error) {
	return map[string]any{"status": "HEALTH_OK", "summary": []string{}}, nil
}

func (Mock) Info(context.Context) (any, error) {
	return map[string]any{"ceph_version": "mock", "total_bytes": nil, "used_bytes": nil, "avail_bytes": nil}, nil
}

func (Mock) Instances(context.Context) (any, error) {
	return map[string]any{"instances": []any{}}, nil
}

func (Mock) Stats(context.Context) (any, error) {
	return map[string]any{"request_count": 0, "error_rate": 0, "p95_latency_ms": 0, "p99_latency_ms": 0}, nil
}

func (Mock) Restart(_ context.Context, instanceID string) (any, error) {
	target := restartTarget(instanceID)
	return map[string]any{"ok": true, "message": "mock restart " + target, "restarted": []string{target}, "error": nil}, nil
}

func (Mock) RollingRestart(context.Context, int) (any, error) {
	return map[string]any{"ok": true, "message": "mock rolling restart", "restarted": []string{}, "error": nil}, nil
}
