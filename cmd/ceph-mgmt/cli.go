package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type CLIConfig struct {
	Bin            string
	Conf           string
	Cluster        string
	CommandTimeout time.Duration
	RestartTimeout time.Duration
}

type CLI struct {
	cfg CLIConfig
}

func NewCLI(cfg CLIConfig) *CLI {
	if cfg.Bin == "" {
		cfg.Bin = "ceph"
	}
	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = 15 * time.Second
	}
	if cfg.RestartTimeout <= 0 {
		cfg.RestartTimeout = 120 * time.Second
	}
	return &CLI{cfg: cfg}
}

func (c *CLI) Health(ctx context.Context) (any, error) {
	var payload map[string]any
	if err := c.runJSON(ctx, c.cfg.CommandTimeout, &payload, "status", "-f", "json"); err != nil {
		return nil, err
	}
	health, _ := payload["health"].(map[string]any)
	return map[string]any{"status": health["status"], "summary": healthSummary(health)}, nil
}

func (c *CLI) Info(ctx context.Context) (any, error) {
	out := map[string]any{"ceph_version": nil, "total_bytes": nil, "used_bytes": nil, "avail_bytes": nil}
	if text, err := c.runText(ctx, c.cfg.CommandTimeout, "version"); err == nil {
		if line := strings.TrimSpace(strings.Split(text, "\n")[0]); line != "" {
			out["ceph_version"] = line
		}
	}
	var payload map[string]any
	if err := c.runJSON(ctx, c.cfg.CommandTimeout, &payload, "df", "-f", "json"); err == nil {
		if stats, ok := payload["stats"].(map[string]any); ok {
			out["total_bytes"] = asInt(stats["total_bytes"])
			used := asInt(stats["total_used_bytes"])
			if used == nil {
				used = asInt(stats["total_used_raw_bytes"])
			}
			out["used_bytes"] = used
			out["avail_bytes"] = asInt(stats["total_avail_bytes"])
		}
	}
	return out, nil
}

func (c *CLI) Instances(ctx context.Context) (any, error) {
	if items, err := c.orchInstances(ctx); err == nil && len(items) > 0 {
		return map[string]any{"instances": items}, nil
	}
	var status map[string]any
	if err := c.runJSON(ctx, c.cfg.CommandTimeout, &status, "status", "-f", "json"); err != nil {
		return nil, err
	}
	return map[string]any{"instances": instancesFromStatus(status)}, nil
}

func (c *CLI) Stats(ctx context.Context) (any, error) {
	names, err := c.daemonNames(ctx)
	if err != nil || len(names) == 0 {
		return emptyStats(), nil
	}
	var dumps []map[string]any
	for _, name := range names {
		var payload map[string]any
		if err := c.runJSON(ctx, c.cfg.CommandTimeout, &payload, "daemon", name, "perf", "dump", "json"); err != nil {
			continue
		}
		dumps = append(dumps, payload)
	}
	if len(dumps) == 0 {
		return emptyStats(), nil
	}
	return aggregateStats(dumps), nil
}

func (c *CLI) Restart(ctx context.Context, instanceID string) (any, error) {
	target := restartTarget(instanceID)
	text, err := c.runText(ctx, c.cfg.RestartTimeout, "orch", "restart", target)
	if err != nil {
		return nil, err
	}
	msg := strings.TrimSpace(text)
	if msg == "" {
		msg = "Restart initiated for " + target
	}
	return map[string]any{"ok": true, "message": msg, "restarted": []string{target}, "error": nil}, nil
}

func (c *CLI) RollingRestart(ctx context.Context, waitSeconds int) (any, error) {
	raw, err := c.Instances(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := raw.(map[string]any)
	list, _ := payload["instances"].([]map[string]any)
	if len(list) == 0 {
		if anyList, ok := payload["instances"].([]any); ok {
			for _, item := range anyList {
				if m, ok := item.(map[string]any); ok {
					list = append(list, m)
				}
			}
		}
	}
	if len(list) == 0 {
		return map[string]any{"ok": false, "message": "No RGW instances found", "restarted": []string{}, "error": "no_instances"}, nil
	}
	restarted := make([]string, 0, len(list))
	for i, item := range list {
		id, _ := item["id"].(string)
		target := restartTarget(id)
		if _, err := c.runText(ctx, c.cfg.RestartTimeout, "orch", "restart", target); err != nil {
			return nil, err
		}
		restarted = append(restarted, target)
		if waitSeconds > 0 && i < len(list)-1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(waitSeconds) * time.Second):
			}
		}
	}
	return map[string]any{
		"ok": true, "message": fmt.Sprintf("Rolling restart completed (%d instances)", len(restarted)),
		"restarted": restarted, "error": nil,
	}, nil
}

func (c *CLI) orchInstances(ctx context.Context) ([]map[string]any, error) {
	var rows []map[string]any
	if err := c.runJSON(ctx, c.cfg.CommandTimeout, &rows, "orch", "ps", "--daemon_type", "rgw", "--format", "json"); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, orchRowToInstance(row))
	}
	return out, nil
}

func (c *CLI) daemonNames(ctx context.Context) ([]string, error) {
	var rows []map[string]any
	if err := c.runJSON(ctx, c.cfg.CommandTimeout, &rows, "orch", "ps", "--daemon_type", "rgw", "--format", "json"); err != nil {
		return nil, err
	}
	var names []string
	for _, row := range rows {
		if s, _ := row["daemon_name"].(string); s != "" {
			names = append(names, s)
			continue
		}
		id, _ := row["daemon_id"].(string)
		if id == "" {
			id, _ = row["name"].(string)
		}
		if id != "" {
			names = append(names, "rgw."+id)
		}
	}
	return names, nil
}

func (c *CLI) runJSON(ctx context.Context, timeout time.Duration, dest any, args ...string) error {
	text, err := c.runText(ctx, timeout, args...)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return json.Unmarshal([]byte(text), dest)
}

func (c *CLI) runText(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.cfg.Bin, c.prefix(args)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("ceph %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func (c *CLI) prefix(args []string) []string {
	out := make([]string, 0, len(args)+4)
	if c.cfg.Conf != "" {
		out = append(out, "--conf", c.cfg.Conf)
	}
	if c.cfg.Cluster != "" {
		out = append(out, "--cluster", c.cfg.Cluster)
	}
	return append(out, args...)
}

func emptyStats() map[string]any {
	return map[string]any{"request_count": nil, "error_rate": nil, "p95_latency_ms": nil, "p99_latency_ms": nil}
}
