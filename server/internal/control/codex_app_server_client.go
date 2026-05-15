package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const codexProtocolLineLimit = 16 * 1024 * 1024

type codexAppServerClient struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	encoder       *json.Encoder
	pending       map[int64]chan codexRPCResponse
	notifications chan codexRPCNotification
	waitDone      chan error
	mu            sync.Mutex
	nextID        int64
}

type codexRPCNotification struct {
	Method string
	Params json.RawMessage
}

type codexRPCResponse struct {
	ID     int64
	Result json.RawMessage
	Error  *codexRPCError
	Err    error
}

type codexRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *codexRPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == 0 {
		return e.Message
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

type codexInitializeResponse struct {
	UserAgent string `json:"userAgent"`
	CodexHome string `json:"codexHome"`
}

type codexThreadStartResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model         string `json:"model"`
	ModelProvider string `json:"modelProvider"`
}

type codexTurnStartResponse struct {
	Turn codexTurn `json:"turn"`
}

type codexTurn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Error  *codexTurnError   `json:"error"`
	Items  []codexThreadItem `json:"items"`
}

type codexTurnError struct {
	Message           string  `json:"message"`
	AdditionalDetails *string `json:"additionalDetails"`
}

func (e *codexTurnError) Error() string {
	if e == nil {
		return ""
	}
	if e.AdditionalDetails != nil && strings.TrimSpace(*e.AdditionalDetails) != "" {
		return strings.TrimSpace(e.Message + "\n" + *e.AdditionalDetails)
	}
	return strings.TrimSpace(e.Message)
}

type codexThreadItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

type codexTurnNotification struct {
	ThreadID string    `json:"threadId"`
	Turn     codexTurn `json:"turn"`
}

type codexItemNotification struct {
	Item     codexThreadItem `json:"item"`
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
}

type codexErrorNotification struct {
	Error     codexTurnError `json:"error"`
	WillRetry bool           `json:"willRetry"`
	ThreadID  string         `json:"threadId"`
	TurnID    string         `json:"turnId"`
}

func (c *codexAppServerClient) request(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	responseCh := make(chan codexRPCResponse, 1)
	c.pending[id] = responseCh
	payload := map[string]any{
		"id":     id,
		"method": method,
	}
	if params != nil {
		payload["params"] = params
	}
	err := c.encoder.Encode(payload)
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return err
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case response := <-responseCh:
		if response.Err != nil {
			return response.Err
		}
		if response.Error != nil {
			return response.Error
		}
		if result != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, result); err != nil {
				return err
			}
		}
		return nil
	}
}

func (c *codexAppServerClient) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), codexProtocolLineLimit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		if rawMethod, ok := envelope["method"]; ok {
			var method string
			if err := json.Unmarshal(rawMethod, &method); err != nil {
				continue
			}
			c.notifications <- codexRPCNotification{Method: method, Params: envelope["params"]}
			continue
		}
		rawID, ok := envelope["id"]
		if !ok {
			continue
		}
		var id int64
		if err := json.Unmarshal(rawID, &id); err != nil {
			continue
		}
		response := codexRPCResponse{ID: id}
		if rawResult, ok := envelope["result"]; ok {
			response.Result = rawResult
		}
		if rawError, ok := envelope["error"]; ok {
			var rpcErr codexRPCError
			if err := json.Unmarshal(rawError, &rpcErr); err == nil {
				response.Error = &rpcErr
			} else {
				response.Err = err
			}
		}
		c.resolveResponse(response)
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.failPending(err)
	close(c.notifications)
}

func (c *codexAppServerClient) resolveResponse(response codexRPCResponse) {
	c.mu.Lock()
	responseCh := c.pending[response.ID]
	delete(c.pending, response.ID)
	c.mu.Unlock()
	if responseCh != nil {
		responseCh <- response
	}
}

func (c *codexAppServerClient) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *codexAppServerClient) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[int64]chan codexRPCResponse{}
	c.mu.Unlock()
	for id, responseCh := range pending {
		responseCh <- codexRPCResponse{ID: id, Err: err}
	}
}

func (c *codexAppServerClient) close() {
	if c == nil || c.cmd == nil {
		return
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	select {
	case <-c.waitDone:
		return
	case <-time.After(500 * time.Millisecond):
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-c.waitDone:
		return
	case <-time.After(2 * time.Second):
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	<-c.waitDone
}

func decodeCodexParams(params json.RawMessage, target any) error {
	if len(params) == 0 {
		return errors.New("empty params")
	}
	return json.Unmarshal(params, target)
}
