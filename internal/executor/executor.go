package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/cronpulser/cronpulser-runner/internal/config"
	"github.com/cronpulser/cronpulser-runner/internal/protocol"
)

const outputLimit = 1024 * 1024

// waitDelay bounds how long Run waits for output pipes after the process has
// exited or been killed, so a background process that escaped the process
// group and kept stdout open cannot block the runner indefinitely.
var waitDelay = 10 * time.Second

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer { return &limitedBuffer{remaining: limit} }

func (w *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	if len(data) > w.remaining {
		data = data[:w.remaining]
		w.truncated = true
	}
	if len(data) > 0 {
		_, _ = w.buffer.Write(data)
		w.remaining -= len(data)
	}
	return original, nil
}

func (w *limitedBuffer) String() string {
	if w.truncated {
		return w.buffer.String() + "\n[output truncated by CronPulser Runner]"
	}
	return w.buffer.String()
}

func Run(parent context.Context, executionID string, job config.Job) protocol.Message {
	started := time.Now().UTC()
	ctx, cancel := context.WithTimeout(parent, job.Timeout.Duration)
	defer cancel()

	command := exec.CommandContext(ctx, job.Command)
	command.Args = append(command.Args, job.Arguments...)
	command.Dir = job.WorkingDirectory
	command.WaitDelay = waitDelay
	stdout, stderr := newLimitedBuffer(outputLimit), newLimitedBuffer(outputLimit)
	command.Stdout, command.Stderr = stdout, stderr
	err := runProcess(command)
	finished := time.Now().UTC()
	duration := int(finished.Sub(started).Milliseconds())
	exitCode := 0
	messageType := protocol.TypeFinished
	var errorText *string
	if err != nil {
		messageType = protocol.TypeFailed
		exitCode = -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			// Windows exit codes are unsigned 32-bit values; report them
			// signed, as Windows tools do; the server stores them as INT.
			exitCode = int(int32(exitError.ExitCode()))
		}
		text := fmt.Sprintf("%s returned exit code %d", job.Command, exitCode)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			text = fmt.Sprintf("execution timed out after %s", job.Timeout.Duration)
		}
		errorText = &text
	}
	stdoutText, stderrText := stdout.String(), stderr.String()
	return protocol.Message{
		Type: messageType, ExecutionID: executionID, Timestamp: finished,
		StartedAt: &started, FinishedAt: &finished, ExitCode: &exitCode,
		DurationMs: &duration, Stdout: &stdoutText, Stderr: &stderrText, Error: errorText,
	}
}
