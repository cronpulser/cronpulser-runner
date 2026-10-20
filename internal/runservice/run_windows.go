//go:build windows

package runservice

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

const serviceName = "CronPulserRunner"

type handler struct {
	run func(context.Context) error
}

type eventWriter struct{ log *eventlog.Log }

func (w eventWriter) Write(message []byte) (int, error) {
	if err := w.log.Info(1, strings.TrimSpace(string(message))); err != nil {
		return 0, err
	}
	return len(message), nil
}

func (h handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-done
				if err != nil && err != context.Canceled {
					return true, 1
				}
				return false, 0
			}
		case err := <-done:
			cancel()
			if err != nil && err != context.Canceled {
				log.Printf("event=runner_stopped error=%q", err)
				return true, 1
			}
			return false, 0
		}
	}
}

func Run(run func(context.Context) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if isService {
		windowsLog, err := eventlog.Open(serviceName)
		if err != nil {
			return err
		}
		defer windowsLog.Close()
		log.SetOutput(eventWriter{log: windowsLog})
		if err := svc.Run(serviceName, handler{run: run}); err != nil {
			_ = windowsLog.Error(2, err.Error())
			return err
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return run(ctx)
}
