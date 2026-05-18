package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	Type             string            `json:"type"`
	ID               string            `json:"id"`
	Text             string            `json:"text"`
	Command          string            `json:"command"`
	Cwd              string            `json:"cwd"`
	Status           json.RawMessage   `json:"status"`
	AggregatedOutput *string           `json:"aggregatedOutput"`
	ExitCode         *int              `json:"exitCode"`
	DurationMs       *int64            `json:"durationMs"`
	Changes          []json.RawMessage `json:"changes"`
	Summary          []string          `json:"summary"`
	Content          []string          `json:"content"`
	Server           string            `json:"server"`
	Tool             string            `json:"tool"`
	Namespace        *string           `json:"namespace"`
}

type codexTurnNotification struct {
	ThreadID string    `json:"threadId"`
	Turn     codexTurn `json:"turn"`
}

type codexThreadStatusNotification struct {
	ThreadID string `json:"threadId"`
	Status   struct {
		Type string `json:"type"`
	} `json:"status"`
}

type codexItemNotification struct {
	Item     codexThreadItem `json:"item"`
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
}

type codexDeltaNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type codexErrorNotification struct {
	Error     codexTurnError `json:"error"`
	WillRetry bool           `json:"willRetry"`
	ThreadID  string         `json:"threadId"`
	TurnID    string         `json:"turnId"`
}

func (a *app) runCodexAppServerSession(ctx context.Context, session agentSession, project project, contextPath string) error {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return errors.New("codex CLI is not available on PATH")
	}

	artifactDir := filepath.Join(session.Workdir, ".mspace", "session")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("create session artifact dir: %w", err)
	}
	session.ArtifactDir = artifactDir
	a.updateSessionArtifactDir(session.ID, artifactDir)
	a.updateSessionAgentStatus(session.ID, "starting app-server")
	a.appendSessionLog(session.ID, "system", "Starting Codex app-server with stdio transport.")

	client, err := a.startCodexAppServer(session, project, contextPath, codexPath)
	if err != nil {
		return err
	}
	defer client.close()

	var initResp codexInitializeResponse
	if err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "mspace",
			"title":   "mspace",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, &initResp); err != nil {
		return fmt.Errorf("initialize codex app-server: %w", err)
	}
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Codex app-server ready: %s", initResp.UserAgent))

	var threadResp codexThreadStartResponse
	if err := client.request(ctx, "thread/start", map[string]any{
		"cwd":                    session.Workdir,
		"approvalPolicy":         "never",
		"approvalsReviewer":      "user",
		"sandbox":                "danger-full-access",
		"developerInstructions":  buildMspaceCodexDeveloperInstructions(),
		"personality":            "pragmatic",
		"ephemeral":              false,
		"sessionStartSource":     "startup",
		"serviceName":            "mspace",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}, &threadResp); err != nil {
		return fmt.Errorf("start codex thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return errors.New("codex app-server returned an empty thread id")
	}
	session.CodexThreadID = threadResp.Thread.ID
	a.updateSessionCodexThread(session.ID, session.CodexThreadID)
	a.updateSessionAgentStatus(session.ID, "thread-started")
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Codex thread: %s", session.CodexThreadID))

	prompt, err := a.buildCodexSessionPrompt(session, project, contextPath, artifactDir)
	if err != nil {
		return fmt.Errorf("build codex prompt: %w", err)
	}

	var turnResp codexTurnStartResponse
	if err := client.request(ctx, "turn/start", map[string]any{
		"threadId": session.CodexThreadID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          prompt,
				"text_elements": []map[string]any{},
			},
		},
		"cwd":            session.Workdir,
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type": "dangerFullAccess",
		},
		"responsesapiClientMetadata": map[string]string{
			"mspace.session_id":    session.ID,
			"mspace.issue_id":      session.IssueID,
			"mspace.project_id":    project.ID,
			"mspace.agent_profile": session.AgentProfile,
		},
	}, &turnResp); err != nil {
		return fmt.Errorf("start codex turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return errors.New("codex app-server returned an empty turn id")
	}
	session.CodexTurnID = turnResp.Turn.ID
	a.updateSessionCodexTurn(session.ID, session.CodexTurnID)
	a.updateSessionAgentStatus(session.ID, "turn-started")
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Codex turn: %s", session.CodexTurnID))

	return a.waitCodexTurn(ctx, client, session)
}

func (a *app) startCodexAppServer(session agentSession, project project, contextPath, codexPath string) (*codexAppServerClient, error) {
	cmd := exec.Command(codexPath, "app-server", "--listen", "stdio://")
	cmd.Dir = session.Workdir
	cmd.Env = append(os.Environ(), a.buildSessionEnv(session, project, contextPath)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	client := &codexAppServerClient{
		cmd:           cmd,
		stdin:         stdin,
		encoder:       json.NewEncoder(stdin),
		pending:       map[int64]chan codexRPCResponse{},
		notifications: make(chan codexRPCNotification, 128),
		waitDone:      make(chan error, 1),
	}
	go client.readLoop(stdout)
	go a.captureCodexDiagnosticStream(session.ID, stderr)
	go func() {
		client.waitDone <- cmd.Wait()
	}()
	return client, nil
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
			c.notifications <- codexRPCNotification{
				Method: method,
				Params: envelope["params"],
			}
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

func (a *app) captureCodexDiagnosticStream(sessionID string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for scanner.Scan() {
		a.appendSessionLog(sessionID, "codex-stderr", truncate(scanner.Text(), 4000))
	}
}

func (a *app) waitCodexTurn(ctx context.Context, client *codexAppServerClient, session agentSession) error {
	outputBuffers := map[string]*strings.Builder{}

	for {
		select {
		case <-ctx.Done():
			a.updateSessionAgentStatus(session.ID, "interrupting")
			if session.CodexThreadID != "" && session.CodexTurnID != "" {
				interruptCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = client.request(interruptCtx, "turn/interrupt", map[string]any{
					"threadId": session.CodexThreadID,
					"turnId":   session.CodexTurnID,
				}, nil)
				cancel()
			}
			return context.Canceled
		case notification, ok := <-client.notifications:
			if !ok {
				return errors.New("codex app-server exited before the turn completed")
			}
			done, err := a.handleCodexNotification(session, notification, outputBuffers)
			if done {
				return err
			}
		}
	}
}

func (a *app) handleCodexNotification(session agentSession, notification codexRPCNotification, outputBuffers map[string]*strings.Builder) (bool, error) {
	switch notification.Method {
	case "thread/status/changed":
		var payload codexThreadStatusNotification
		if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == session.CodexThreadID {
			a.updateSessionAgentStatus(session.ID, "thread-"+payload.Status.Type)
		}
	case "turn/started":
		var payload codexTurnNotification
		if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == session.CodexThreadID {
			if payload.Turn.ID != "" && payload.Turn.ID != session.CodexTurnID {
				session.CodexTurnID = payload.Turn.ID
				a.updateSessionCodexTurn(session.ID, payload.Turn.ID)
			}
			a.updateSessionAgentStatus(session.ID, "turn-"+payload.Turn.Status)
		}
	case "item/started":
		var payload codexItemNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(session, payload.ThreadID, payload.TurnID) {
			a.logCodexItemStarted(session.ID, payload.Item)
		}
	case "item/commandExecution/outputDelta":
		var payload codexDeltaNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(session, payload.ThreadID, payload.TurnID) {
			builder := outputBuffers[payload.ItemID]
			if builder == nil {
				builder = &strings.Builder{}
				outputBuffers[payload.ItemID] = builder
			}
			builder.WriteString(payload.Delta)
		}
	case "item/completed":
		var payload codexItemNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(session, payload.ThreadID, payload.TurnID) {
			a.logCodexItemCompleted(session.ID, payload.Item, outputBuffers[payload.Item.ID])
			delete(outputBuffers, payload.Item.ID)
		}
	case "error":
		var payload codexErrorNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(session, payload.ThreadID, payload.TurnID) {
			message := payload.Error.Error()
			if message == "" {
				message = "Codex app-server reported an unknown error."
			}
			a.appendSessionLog(session.ID, "codex-error", message)
			if !payload.WillRetry {
				return true, errors.New(message)
			}
		}
	case "turn/completed":
		var payload codexTurnNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(session, payload.ThreadID, payload.Turn.ID) {
			a.updateSessionAgentStatus(session.ID, "turn-"+payload.Turn.Status)
			switch payload.Turn.Status {
			case "completed":
				return true, nil
			case "interrupted":
				return true, context.Canceled
			case "failed":
				if payload.Turn.Error != nil && payload.Turn.Error.Error() != "" {
					return true, errors.New(payload.Turn.Error.Error())
				}
				return true, errors.New("codex turn failed")
			default:
				return true, fmt.Errorf("codex turn ended with status %s", payload.Turn.Status)
			}
		}
	}
	return false, nil
}

func (a *app) logCodexItemStarted(sessionID string, item codexThreadItem) {
	switch item.Type {
	case "commandExecution":
		a.updateSessionAgentStatus(sessionID, "running-command")
		if strings.TrimSpace(item.Command) != "" {
			a.appendSessionLog(sessionID, "command", "$ "+strings.TrimSpace(item.Command))
		}
	case "agentMessage":
		a.updateSessionAgentStatus(sessionID, "writing")
	case "reasoning":
		a.updateSessionAgentStatus(sessionID, "reasoning")
	case "fileChange":
		a.updateSessionAgentStatus(sessionID, "editing-files")
	}
}

func (a *app) logCodexItemCompleted(sessionID string, item codexThreadItem, bufferedOutput *strings.Builder) {
	switch item.Type {
	case "agentMessage":
		if strings.TrimSpace(item.Text) != "" {
			a.appendSessionLog(sessionID, "agent", item.Text)
		}
	case "plan":
		if strings.TrimSpace(item.Text) != "" {
			a.appendSessionLog(sessionID, "plan", item.Text)
		}
	case "reasoning":
		summary := strings.TrimSpace(strings.Join(item.Summary, "\n"))
		if summary != "" {
			a.appendSessionLog(sessionID, "reasoning", summary)
		}
	case "commandExecution":
		output := ""
		if item.AggregatedOutput != nil {
			output = *item.AggregatedOutput
		} else if bufferedOutput != nil {
			output = bufferedOutput.String()
		}
		status := "completed"
		if item.ExitCode != nil {
			status = fmt.Sprintf("exit %d", *item.ExitCode)
		}
		if strings.TrimSpace(output) != "" {
			a.appendSessionLog(sessionID, "command", truncate(fmt.Sprintf("Command %s:\n%s", status, output), 12000))
		} else {
			a.appendSessionLog(sessionID, "command", "Command "+status+".")
		}
	case "fileChange":
		if len(item.Changes) > 0 {
			a.appendSessionLog(sessionID, "file", fmt.Sprintf("Applied %d file change(s).", len(item.Changes)))
		}
	case "mcpToolCall":
		tool := strings.TrimSpace(item.Server + "." + item.Tool)
		a.appendSessionLog(sessionID, "tool", strings.TrimSpace(fmt.Sprintf("MCP tool completed: %s", tool)))
	case "dynamicToolCall":
		namespace := ""
		if item.Namespace != nil {
			namespace = *item.Namespace
		}
		tool := strings.TrimSpace(namespace + "." + item.Tool)
		a.appendSessionLog(sessionID, "tool", strings.TrimSpace(fmt.Sprintf("Tool completed: %s", tool)))
	}
}

func sameCodexTurn(session agentSession, threadID, turnID string) bool {
	if session.CodexThreadID != "" && threadID != session.CodexThreadID {
		return false
	}
	return session.CodexTurnID == "" || turnID == session.CodexTurnID
}

func decodeCodexParams(params json.RawMessage, target any) error {
	if len(params) == 0 {
		return errors.New("empty params")
	}
	return json.Unmarshal(params, target)
}

func buildMspaceCodexDeveloperInstructions() string {
	return strings.TrimSpace(`
You are running as a Codex coding agent inside an mspace local agent session.

Follow these mspace rules:
- Work in the prepared git worktree for this session.
- Inspect the repository before changing code.
- Keep changes focused on the issue and avoid unrelated refactors.
- Do not commit, push, create a pull request, or delete the session worktree unless the issue or session instructions explicitly ask for it.
- If Kubernetes validation is needed, use only the configured context, kubeconfig, and issue namespace. Creating or deleting the explicitly named issue test namespace is allowed only when the current turn asks for deploy or cleanup. Do not read Secrets.
- Run relevant tests or validation commands when practical, and report exactly what passed or failed.
- Answer directly. Do not introduce yourself. Do not state that you saw the current comment, issue history, or prior sessions unless the user explicitly asks what context you received.
- Finish with a concise answer or, when you changed code, a concise summary of changes, validation, and remaining risks.
`)
}

func (a *app) buildCodexSessionPrompt(session agentSession, project project, contextPath, artifactDir string) (string, error) {
	detail, err := a.loadIssueDetail(session.IssueID)
	if err != nil {
		return "", err
	}
	profile, err := a.loadAgentProfile(session.AgentProfile)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("# mspace Agent Session\n\n")
	builder.WriteString("You are assigned to this mspace issue. Use the current working directory as the prepared session worktree.\n\n")

	builder.WriteString("## Agent Profile\n\n")
	builder.WriteString(fmt.Sprintf("- Profile: %s (%s)\n", profile.Name, profile.Mention))
	builder.WriteString(profile.Instructions)
	builder.WriteString("\n\n")

	if strings.TrimSpace(session.Command) != "" {
		builder.WriteString("## Current Turn Request\n\n")
		builder.WriteString(fmt.Sprintf("This turn was triggered by a newly added `%s` issue comment. The text below is the highest-priority request for this turn. Treat it like the current Multica-style triggering comment inside the existing timeline: find it in the comments, answer or act on this comment first, and use the original issue, older comments, and prior sessions only as context. Do not confuse this request with the original issue body or repeat already-completed work unless this request asks you to.\n\n", profile.Mention))
		builder.WriteString(strings.TrimSpace(session.Command))
		builder.WriteString("\n\n")
	}

	builder.WriteString("## Issue\n\n")
	builder.WriteString(fmt.Sprintf("- ID: %s\n", detail.Issue.ID))
	builder.WriteString(fmt.Sprintf("- Title: %s\n", detail.Issue.Title))
	builder.WriteString(fmt.Sprintf("- Status: %s\n", detail.Issue.Status))
	builder.WriteString(fmt.Sprintf("- Labels: %s\n", formatIssueLabels(detail.Labels)))
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(detail.Issue.Body))
	builder.WriteString("\n\n")

	writeIssueTaskList(&builder, detail.ChildIssues)

	builder.WriteString("## Project\n\n")
	builder.WriteString(fmt.Sprintf("- Name: %s\n", project.Name))
	builder.WriteString(fmt.Sprintf("- Repository: %s\n", project.RepoPath))
	builder.WriteString(fmt.Sprintf("- Worktree: %s\n", session.Workdir))
	builder.WriteString(fmt.Sprintf("- Branch: %s\n", session.Branch))
	builder.WriteString(fmt.Sprintf("- Default branch: %s\n", project.DefaultBranch))
	builder.WriteString(fmt.Sprintf("- Session context file: %s\n", contextPath))
	builder.WriteString(fmt.Sprintf("- Artifact directory: %s\n", artifactDir))
	builder.WriteString("\n")

	if runbook, err := a.loadProjectRunbook(project.ID); err == nil {
		writeProjectRunbookPromptSection(&builder, runbook)
	}

	builder.WriteString("## Validation Context\n\n")
	builder.WriteString(fmt.Sprintf("- Kubernetes context: %s\n", valueOrUnset(project.KubeContext)))
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", valueOrUnset(project.KubeconfigPath)))
	builder.WriteString(fmt.Sprintf("- Project fallback namespace: %s\n", valueOrUnset(project.Namespace)))
	builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", valueOrUnset(project.ImageRegistryPrefix)))
	builder.WriteString(fmt.Sprintf("- Preview domain: %s\n", valueOrUnset(project.PreviewDomain)))
	builder.WriteString(fmt.Sprintf("- Ingress class: %s\n", valueOrUnset(project.IngressClass)))
	builder.WriteString(fmt.Sprintf("- Node host: %s\n", valueOrUnset(project.NodeHost)))
	builder.WriteString("\n")

	if detail.TestEnvironment != nil {
		builder.WriteString("## Issue Test Environment\n\n")
		builder.WriteString(fmt.Sprintf("- Namespace: %s\n", detail.TestEnvironment.Namespace))
		builder.WriteString(fmt.Sprintf("- Namespace status: %s\n", detail.TestEnvironment.NamespaceStatus))
		builder.WriteString(fmt.Sprintf("- Cleanup status: %s\n", detail.TestEnvironment.CleanupStatus))
		builder.WriteString(fmt.Sprintf("- Preview URL: %s\n", valueOrUnset(detail.TestEnvironment.PreviewURL)))
		builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", valueOrUnset(detail.TestEnvironment.ImageRegistryPrefix)))
		builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", valueOrUnset(detail.TestEnvironment.KubeconfigPath)))
		builder.WriteString(fmt.Sprintf("- Preview strategy: %s\n", previewStrategyLabel(*detail.TestEnvironment)))
		builder.WriteString("\n")
	}

	builder.WriteString("## Issue Timeline Comments\n\n")
	builder.WriteString(fmt.Sprintf("Comments are listed oldest to newest. If a recent human comment contains `%s`, treat that as the triggering comment for this turn.\n\n", profile.Mention))
	if len(detail.Comments) == 0 {
		builder.WriteString("(no comments)\n\n")
	} else {
		for i := len(detail.Comments) - 1; i >= 0; i-- {
			comment := detail.Comments[i]
			builder.WriteString(fmt.Sprintf("### %s at %s\n\n", comment.AuthorType, comment.CreatedAt))
			builder.WriteString(strings.TrimSpace(comment.Body))
			builder.WriteString("\n\n")
		}
	}

	builder.WriteString("## Prior Sessions\n\n")
	priorSessionCount := 0
	for _, priorSession := range detail.Sessions {
		if priorSession.ID == session.ID {
			continue
		}
		priorSessionCount++
		priorProfile, _ := a.loadAgentProfile(priorSession.AgentProfile)
		sessionLabel := priorSession.Provider
		if priorProfile.Name != "" {
			sessionLabel = priorProfile.Name
		}
		builder.WriteString(fmt.Sprintf("### %s session %s\n\n", sessionLabel, shortID(priorSession.ID)))
		builder.WriteString(fmt.Sprintf("- Status: %s\n", priorSession.Status))
		builder.WriteString(fmt.Sprintf("- Agent profile: %s\n", valueOrUnset(priorSession.AgentProfile)))
		builder.WriteString(fmt.Sprintf("- Agent status: %s\n", valueOrUnset(priorSession.AgentStatus)))
		builder.WriteString(fmt.Sprintf("- Branch: %s\n", valueOrUnset(priorSession.Branch)))
		builder.WriteString(fmt.Sprintf("- Started: %s\n", priorSession.CreatedAt))
		builder.WriteString(fmt.Sprintf("- Updated: %s\n", priorSession.UpdatedAt))
		if strings.TrimSpace(priorSession.Command) != "" {
			builder.WriteString(fmt.Sprintf("- Request: %s\n", strings.TrimSpace(priorSession.Command)))
		}
		logs, err := a.listSessionLogs(priorSession.ID)
		if err != nil {
			return "", err
		}
		for _, summary := range summarizeSessionLogs(logs) {
			builder.WriteString(fmt.Sprintf("- %s\n", summary))
		}
		builder.WriteString("\n")
	}
	if priorSessionCount == 0 {
		builder.WriteString("(no prior sessions)\n\n")
	}

	builder.WriteString("## Expected Output\n\n")
	builder.WriteString("Respond to the current turn request in the context of the issue timeline. If code changes are requested, implement them as far as practical, run relevant validation, and finish with a concise summary. If the current turn is only a question, greeting, acknowledgement, or status check, answer it directly instead of re-running the original issue. Do not introduce yourself. Do not say you saw the current comment, Issue history, or prior sessions unless the user explicitly asks what context you received. Do not include a fresh agent mention in your final response unless the user explicitly asks you to trigger another agent turn. The mspace runner will keep this worktree, stream your app-server events, and collect Kubernetes evidence after the turn completes.\n\n")
	builder.WriteString("If you make source-code changes, write `${MSPACE_SESSION_ARTIFACT_DIR}/branch-name.json` before finishing. Use JSON like `{ \"branch\": \"fix/short-semantic-name\" }`. The branch must use a Conventional Commit type prefix such as `feat/`, `fix/`, `chore/`, `docs/`, `refactor/`, `test/`, `perf/`, `build/`, or `ci/`, and the slug should summarize the actual diff in lowercase words separated by hyphens.\n")
	builder.WriteString("Also write `${MSPACE_SESSION_ARTIFACT_DIR}/review-evidence.json` when practical. Use JSON with keys `commandsRun`, `tests`, `buildResult`, `deploymentResult`, `agentSummary`, `risks`, and `followUps`. Keep it factual; leave sections empty when not applicable.\n")
	builder.WriteString("When you discover or correct durable project operation knowledge, write `${MSPACE_SESSION_ARTIFACT_DIR}/project-runbook.md` as Markdown. Include useful sections such as Dependencies, Local Start, Tests, Build, Image Build, Deploy, Health Check, and Common Failures. Do not edit repository docs for this unless the user asks.\n")
	builder.WriteString("Issue status rules: only a human may close or cancel the top-level issue. Never set the top-level issue status to `closed` or `cancelled`. Do not use issue status for transient session progress. When you need to report workflow readiness through the mspace API, use the scoped `MSPACE_AGENT_TOKEN` as a bearer token and choose one of `needs_review`, `ready_for_test`, or `blocked`.\n")

	return builder.String(), nil
}

func summarizeSessionLogs(logs []sessionLog) []string {
	summaries := []string{}
	for i := len(logs) - 1; i >= 0 && len(summaries) < 4; i-- {
		log := logs[i]
		message := strings.TrimSpace(log.Message)
		if message == "" {
			continue
		}
		switch log.Stream {
		case "agent":
			summaries = append(summaries, "Agent said: "+singleLineSummary(message, 360))
		case "file":
			summaries = append(summaries, "File activity: "+singleLineSummary(message, 240))
		case "system":
			if strings.Contains(message, "completed") || strings.Contains(message, "failed") || strings.Contains(message, "cancelled") {
				summaries = append(summaries, "System: "+singleLineSummary(message, 240))
			}
		}
	}
	for left, right := 0, len(summaries)-1; left < right; left, right = left+1, right-1 {
		summaries[left], summaries[right] = summaries[right], summaries[left]
	}
	return summaries
}

func singleLineSummary(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func valueOrUnset(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not configured"
	}
	return value
}
