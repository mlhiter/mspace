package main

import (
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type agentEngineProcess struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	done        chan struct{}
	mu          sync.Mutex
	err         error
	outputs     []io.Closer
	outputsOnce sync.Once
	processTree io.Closer
	treeOnce    sync.Once
}

func startAgentEngineProcess(cmd *exec.Cmd, stdin io.WriteCloser) (*agentEngineProcess, io.Reader, io.Reader, error) {
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return nil, nil, nil, err
	}
	processTree, err := attachAgentEngineProcessTree(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return nil, nil, nil, err
	}
	process := &agentEngineProcess{
		cmd:         cmd,
		stdin:       stdin,
		done:        make(chan struct{}),
		outputs:     []io.Closer{stdoutWriter, stderrWriter},
		processTree: processTree,
	}
	go func() {
		err := cmd.Wait()
		process.closeProcessTree()
		process.closeOutputs()
		process.mu.Lock()
		process.err = err
		process.mu.Unlock()
		close(process.done)
	}()
	return process, stdoutReader, stderrReader, nil
}

func (p *agentEngineProcess) waitError() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *agentEngineProcess) stop(grace time.Duration) {
	if p == nil || p.cmd == nil {
		return
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	select {
	case <-p.done:
		return
	default:
	}
	interruptAgentEngineProcess(p.cmd)
	p.closeOutputs()
	if grace > 0 {
		select {
		case <-p.done:
			return
		case <-time.After(grace):
		}
	}
	p.closeProcessTree()
	killAgentEngineProcess(p.cmd)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
}

func (p *agentEngineProcess) closeProcessTree() {
	if p == nil {
		return
	}
	p.treeOnce.Do(func() {
		if p.processTree != nil {
			_ = p.processTree.Close()
		}
	})
}

func (p *agentEngineProcess) closeOutputs() {
	if p == nil {
		return
	}
	p.outputsOnce.Do(func() {
		for _, output := range p.outputs {
			_ = output.Close()
		}
	})
}

func defaultAgentEngineEnv(engine string, extra map[string]string) []string {
	return buildAgentEngineEnvForEngine(engine, os.Environ(), extra)
}

func clearWorkerCredentialEnvironment() {
	for _, key := range []string{"MSPACE_RUNTIME_TOKEN", "MSPACE_RUNTIME_TOKEN_FILE"} {
		_ = os.Unsetenv(key)
	}
}

// buildAgentEngineEnvForEngine treats payload env as an explicit session
// grant. Inherited Worker env must match the runtime or engine-auth allowlist;
// both sources are still subject to the non-bypassable control-plane denylist.
func buildAgentEngineEnvForEngine(engine string, parent []string, extra map[string]string) []string {
	values := map[string]string{}
	for _, entry := range parent {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !isInheritedAgentEngineEnvironmentKey(engine, key) || isForbiddenAgentEngineEnvironmentKey(engine, key) {
			continue
		}
		values[canonicalEnvironmentKey(key)] = key + "=" + value
	}
	for key, value := range extra {
		if isForbiddenAgentEngineEnvironmentKey(engine, key) {
			continue
		}
		values[canonicalEnvironmentKey(key)] = key + "=" + value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}

func canonicalEnvironmentKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}

func isInheritedAgentEngineEnvironmentKey(engine, key string) bool {
	key = canonicalEnvironmentKey(key)
	if isAgentEngineRuntimeEnvironmentKey(key) || isAgentEngineAuthEnvironmentKey(engine, key) {
		return true
	}
	return strings.HasPrefix(key, "LC_") || strings.HasPrefix(key, "XDG_")
}

func isAgentEngineRuntimeEnvironmentKey(key string) bool {
	switch key {
	case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TMP", "TEMP",
		"LANG", "LANGUAGE", "TERM", "COLORTERM", "NO_COLOR", "FORCE_COLOR",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS",
		"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
		"APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)":
		return true
	default:
		return false
	}
}

func isAgentEngineAuthEnvironmentKey(engine, key string) bool {
	key = canonicalEnvironmentKey(key)
	if engine == "" {
		return isAgentEngineAuthEnvironmentKey(agentEngineCodex, key) ||
			isAgentEngineAuthEnvironmentKey(agentEngineClaudeCode, key) ||
			isAgentEngineAuthEnvironmentKey(agentEnginePi, key)
	}
	switch engine {
	case agentEngineCodex:
		switch key {
		case "CODEX_HOME", "OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_ORGANIZATION", "OPENAI_ORG_ID",
			"OPENAI_PROJECT", "OPENAI_PROJECT_ID", "AZURE_OPENAI_API_KEY", "AZURE_OPENAI_ENDPOINT", "OPENAI_API_VERSION":
			return true
		}
	case agentEngineClaudeCode:
		switch key {
		case "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
			"CLAUDE_CODE_USE_FOUNDRY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL",
			"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION",
			"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "GOOGLE_APPLICATION_CREDENTIALS", "CLOUD_ML_REGION",
			"ANTHROPIC_VERTEX_PROJECT_ID":
			return true
		}
	case agentEnginePi:
		switch key {
		case "PI_CODING_AGENT_DIR", "PI_CONFIG_DIR", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
			"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENROUTER_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY",
			"MISTRAL_API_KEY", "GROQ_API_KEY", "XAI_API_KEY", "COHERE_API_KEY", "DEEPSEEK_API_KEY",
			"AZURE_OPENAI_API_KEY", "AZURE_OPENAI_ENDPOINT", "OPENAI_API_VERSION",
			"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION",
			"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "GOOGLE_APPLICATION_CREDENTIALS":
			return true
		}
	}
	return false
}

func isForbiddenAgentEngineEnvironmentKey(engine, key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	if key == "MSPACE_AGENT_TOKEN" || isAgentEngineAuthEnvironmentKey(engine, key) {
		return false
	}
	if strings.HasPrefix(key, "MSPACE_RUNTIME_") || strings.HasPrefix(key, "MSPACE_WORKER_") ||
		strings.HasPrefix(key, "MSPACE_BOOTSTRAP_") || strings.HasPrefix(key, "MSPACE_GITHUB_") ||
		strings.HasPrefix(key, "POSTGRES_") || strings.HasPrefix(key, "PG") {
		return true
	}
	switch key {
	case "MSPACE_SERVER_URL", "MSPACE_CONTROL_PLANE_URL", "MSPACE_STORE", "MSPACE_SQLITE_PATH",
		"DATABASE_URL", "GH_TOKEN", "GITHUB_TOKEN", "GITHUB_APP_PRIVATE_KEY":
		return true
	}
	return strings.Contains(key, "PASSWORD") || strings.Contains(key, "PRIVATE_KEY") ||
		strings.Contains(key, "CLIENT_SECRET") || strings.HasSuffix(key, "_SECRET") || strings.HasSuffix(key, "_TOKEN")
}
