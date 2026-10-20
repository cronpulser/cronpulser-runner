package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/cronpulser/cronpulser-runner/internal/cache"
	"github.com/cronpulser/cronpulser-runner/internal/config"
	"github.com/cronpulser/cronpulser-runner/internal/executor"
	"github.com/cronpulser/cronpulser-runner/internal/protocol"
	"github.com/gorilla/websocket"
)

var reconnectDelays = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

// outbound is a message queued for the current connection. Results are final
// job results that must reach the server once: the write loop skips a result
// that an earlier connection already delivered.
type outbound struct {
	message protocol.Message
	result  bool
}

type Client struct {
	config  *config.Config
	cache   *cache.Cache
	version string
	// executions and results outlive single connections, so a running job
	// survives a dropped connection and its result is sent after reconnecting.
	executions chan protocol.Message
	results    chan outbound
}

func New(cfg *config.Config, executionCache *cache.Cache, version string) *Client {
	return &Client{
		config: cfg, cache: executionCache, version: version,
		executions: make(chan protocol.Message, 64),
		results:    make(chan outbound, 64),
	}
}

func messageID() string {
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	return "msg_" + hex.EncodeToString(buffer)
}

func isFinal(messageType string) bool {
	return messageType == protocol.TypeFinished || messageType == protocol.TypeFailed
}

func websocketURL(server string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(server, "/"))
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", errors.New("server URL must use http or https")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/runner/connect"
	return parsed.String(), nil
}

func (c *Client) Run(ctx context.Context) error {
	go c.executionLoop(ctx)
	attempt := 0
	for ctx.Err() == nil {
		connectedAt := time.Now()
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("event=disconnected error=%q", err)
		if time.Since(connectedAt) > time.Minute {
			attempt = 0
		}
		delay := reconnectDelays[attempt]
		if attempt < len(reconnectDelays)-1 {
			attempt++
		}
		log.Printf("event=reconnecting delay=%s", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

// publish hands a message from the execution loop to the current connection.
// It never blocks: a dropped result stays pending in the cache and is sent
// again after the next connect.
func (c *Client) publish(item outbound) {
	select {
	case c.results <- item:
	default:
	}
}

func (c *Client) connect(ctx context.Context) error {
	connectionContext, cancelConnection := context.WithCancel(ctx)
	defer cancelConnection()
	endpoint, err := websocketURL(c.config.Server)
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.config.RunnerSecret)
	headers.Set("X-Runner-Id", c.config.RunnerID)
	connection, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if response != nil {
			return fmt.Errorf("connect: server returned %s", response.Status)
		}
		return fmt.Errorf("connect: %w", err)
	}
	defer connection.Close()
	log.Printf("event=connected server=%q", c.config.Server)

	outgoing := make(chan outbound, 64)
	errorsChannel := make(chan error, 2)
	send := func(item outbound) bool {
		select {
		case outgoing <- item:
			return true
		case <-connectionContext.Done():
			return false
		}
	}
	connection.SetReadLimit(64 * 1024)
	_ = connection.SetReadDeadline(time.Now().Add(90 * time.Second))
	connection.SetPongHandler(func(string) error { return connection.SetReadDeadline(time.Now().Add(90 * time.Second)) })

	hostname, _ := os.Hostname()
	outgoing <- outbound{message: protocol.Message{Type: protocol.TypeHello, MessageID: messageID(), Timestamp: time.Now().UTC(), RunnerID: c.config.RunnerID, Version: c.version, Hostname: hostname, OS: runtime.GOOS + "/" + runtime.GOARCH, Jobs: c.config.JobKeys()}}

	go c.writeLoop(connectionContext, connection, outgoing, errorsChannel)
	go func() {
		for _, pending := range c.cache.Pending() {
			if !send(outbound{message: pending, result: true}) {
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case item := <-c.results:
				if !send(item) {
					return
				}
			case <-connectionContext.Done():
				return
			}
		}
	}()
	go func() {
		for {
			var message protocol.Message
			if err := connection.ReadJSON(&message); err != nil {
				errorsChannel <- err
				return
			}
			if message.Type != protocol.TypeExecute {
				continue
			}
			if previous, ok := c.cache.Get(message.ExecutionID); ok {
				replay := previous.Message
				replay.MessageID, replay.Timestamp = messageID(), time.Now().UTC()
				if !send(outbound{message: replay}) {
					return
				}
				continue
			}
			_, ok := c.config.Jobs[message.JobKey]
			if !ok {
				now := time.Now().UTC()
				exitCode := -1
				errorText := "job key is not registered on this runner"
				failure := protocol.Message{Type: protocol.TypeFailed, MessageID: messageID(), Timestamp: now, ExecutionID: message.ExecutionID, FinishedAt: &now, ExitCode: &exitCode, Error: &errorText}
				_ = c.cache.Put(message.ExecutionID, protocol.TypeFailed, failure)
				if !send(outbound{message: failure, result: true}) {
					return
				}
				continue
			}
			accepted := protocol.Message{Type: protocol.TypeAccepted, MessageID: messageID(), Timestamp: time.Now().UTC(), ExecutionID: message.ExecutionID}
			if err := c.cache.Put(message.ExecutionID, protocol.TypeAccepted, accepted); err != nil {
				log.Printf("event=cache_error error=%q", err)
				continue
			}
			if !send(outbound{message: accepted}) {
				return
			}
			select {
			case c.executions <- message:
			case <-connectionContext.Done():
				return
			}
		}
	}()

	select {
	case err := <-errorsChannel:
		return err
	case <-ctx.Done():
		return nil
	}
}

func (c *Client) writeLoop(ctx context.Context, connection *websocket.Conn, outgoing <-chan outbound, errorsChannel chan<- error) {
	heartbeat := time.NewTicker(30 * time.Second)
	ping := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	defer ping.Stop()
	for {
		select {
		case item := <-outgoing:
			message := item.message
			if item.result && !c.cache.IsPending(message.ExecutionID) {
				continue
			}
			if message.MessageID == "" {
				message.MessageID = messageID()
			}
			if message.Timestamp.IsZero() {
				message.Timestamp = time.Now().UTC()
			}
			_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := connection.WriteJSON(message); err != nil {
				errorsChannel <- err
				return
			}
			if isFinal(message.Type) && message.ExecutionID != "" {
				if err := c.cache.MarkDelivered(message.ExecutionID); err != nil {
					log.Printf("event=cache_error error=%q", err)
				}
			}
		case <-heartbeat.C:
			message := protocol.Message{Type: protocol.TypeHeartbeat, MessageID: messageID(), Timestamp: time.Now().UTC(), RunnerID: c.config.RunnerID}
			_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := connection.WriteJSON(message); err != nil {
				errorsChannel <- err
				return
			}
		case <-ping.C:
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				errorsChannel <- err
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// executionLoop runs accepted jobs one at a time on the service context, so a
// dropped connection does not interrupt a running job.
func (c *Client) executionLoop(ctx context.Context) {
	for {
		select {
		case request := <-c.executions:
			job, exists := c.config.Jobs[request.JobKey]
			if !exists {
				continue
			}
			startedAt := time.Now().UTC()
			started := protocol.Message{Type: protocol.TypeStarted, MessageID: messageID(), Timestamp: startedAt, ExecutionID: request.ExecutionID, StartedAt: &startedAt}
			_ = c.cache.Put(request.ExecutionID, protocol.TypeStarted, started)
			c.publish(outbound{message: started})
			result := executor.Run(ctx, request.ExecutionID, job)
			result.MessageID = messageID()
			if err := c.cache.Put(request.ExecutionID, result.Type, result); err != nil {
				// Without a cache entry the result cannot be tracked as pending,
				// so send it untracked rather than not at all.
				log.Printf("event=cache_error error=%q", err)
				c.publish(outbound{message: result})
				continue
			}
			c.publish(outbound{message: result, result: true})
		case <-ctx.Done():
			return
		}
	}
}
