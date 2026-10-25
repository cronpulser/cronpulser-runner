package protocol

import "time"

const (
	TypeHello     = "runner.hello"
	TypeHeartbeat = "runner.heartbeat"
	TypeExecute   = "job.execute"
	TypeAccepted  = "job.accepted"
	TypeStarted   = "job.started"
	TypeFinished  = "job.finished"
	TypeFailed    = "job.failed"
)

type Message struct {
	Type        string     `json:"type" yaml:"-"`
	MessageID   string     `json:"messageId" yaml:"-"`
	Timestamp   time.Time  `json:"timestamp" yaml:"-"`
	RunnerID    string     `json:"runnerId,omitempty" yaml:"-"`
	Version     string     `json:"version,omitempty" yaml:"-"`
	Hostname    string     `json:"hostname,omitempty" yaml:"-"`
	OS          string     `json:"os,omitempty" yaml:"-"`
	Jobs        []string   `json:"jobs,omitempty" yaml:"-"`
	ExecutionID string     `json:"executionId,omitempty" yaml:"-"`
	JobKey      string     `json:"jobKey,omitempty" yaml:"-"`
	StartedAt   *time.Time `json:"startedAt,omitempty" yaml:"-"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty" yaml:"-"`
	ExitCode    *int       `json:"exitCode,omitempty" yaml:"-"`
	DurationMs  *int       `json:"durationMs,omitempty" yaml:"-"`
	Stdout      *string    `json:"stdout,omitempty" yaml:"-"`
	Stderr      *string    `json:"stderr,omitempty" yaml:"-"`
	Error       *string    `json:"error,omitempty" yaml:"-"`
}
