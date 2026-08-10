package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/yoann/kern-orch/internal/skills"
)

// Runner invokes a tool skill's declared command in a subprocess, mirroring
// agentrunner.Subprocess's shape but for a single request/response call rather than a
// streamed one — a tool answers once, it does not narrate.
type Runner struct {
	Env    []string  // child environment (nil => inherit parent)
	Stderr io.Writer // child stderr (nil => discarded)
}

// Invoke validates input against sk's declared params, then runs sk's command, sending
// input on stdin and reading one Response line from stdout.
func (r *Runner) Invoke(ctx context.Context, sk skills.Skill, input map[string]any) (Result, error) {
	if sk.Type != skills.TypeTool {
		return Result{}, fmt.Errorf("tools: skill %q is not a tool", sk.Name)
	}
	if len(sk.Command) == 0 {
		return Result{}, fmt.Errorf("tools: skill %q has no command", sk.Name)
	}
	if err := Validate(sk, input); err != nil {
		return Result{}, err
	}

	payload, err := json.Marshal(Request{Input: input})
	if err != nil {
		return Result{}, fmt.Errorf("tools: marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, sk.Command[0], sk.Command[1:]...)
	cmd.Env = r.Env
	cmd.Stderr = r.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, fmt.Errorf("tools: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("tools: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("tools: start %q: %w", sk.Command[0], err)
	}

	if _, err := stdin.Write(payload); err != nil {
		_ = cmd.Wait()
		return Result{}, fmt.Errorf("tools: write request: %w", err)
	}
	_ = stdin.Close()

	resp, readErr := readResponse(stdout)
	if waitErr := cmd.Wait(); waitErr != nil && readErr == nil {
		return Result{}, fmt.Errorf("tools: %q exited: %w", sk.Command[0], waitErr)
	}
	if readErr != nil {
		return Result{}, readErr
	}
	if resp.Error != "" {
		return Result{}, fmt.Errorf("tools: %s: %s", sk.Name, resp.Error)
	}
	return Result{Label: resp.Label, Value: resp.Value, AsOf: time.Now()}, nil
}

// readResponse reads exactly one JSON line — a tool answers once.
func readResponse(stdout io.Reader) (Response, error) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			return Response{}, fmt.Errorf("tools: bad response %q: %w", line, err)
		}
		return resp, nil
	}
	if err := sc.Err(); err != nil {
		return Response{}, fmt.Errorf("tools: read stdout: %w", err)
	}
	return Response{}, fmt.Errorf("tools: child produced no response")
}
