package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var authFailurePattern = regexp.MustCompile(`(?i)not logged in|not authenticated|unauthorized|login required|please (?:log|sign) in|authentication (?:failed|required)|invalid (?:access |refresh )?token|token (?:expired|invalid)|re-authentication required`)

type Client struct {
	cfg    config.Config
	logger *zap.Logger
}

type manifest struct {
	RunID        string    `json:"run_id"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	Status       string    `json:"status"`
	WindowStart  time.Time `json:"window_start"`
	WindowEnd    time.Time `json:"window_end"`
	Accounts     []string  `json:"accounts"`
	Model        string    `json:"model"`
	SessionID    string    `json:"session_id"`
	ResultPath   string    `json:"result_path,omitempty"`
	ExitCode     int       `json:"exit_code,omitempty"`
	TimedOut     bool      `json:"timed_out,omitempty"`
	Partial      bool      `json:"partial,omitempty"`
	ErrorCode    string    `json:"error_code,omitempty"`
	OutputCutoff bool      `json:"output_truncated,omitempty"`
}

var Module = fx.Module(
	"grok",
	fx.Provide(
		fx.Annotate(New, fx.As(new(ports.Searcher))),
	),
)

func New(cfg config.Config, logger *zap.Logger) (*Client, error) {
	if err := ensurePrivateDirectory(cfg.Storage.RunsDirectory); err != nil {
		return nil, fmt.Errorf("create Grok runs directory: %w", err)
	}
	absolute, err := filepath.Abs(cfg.Storage.RunsDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve Grok runs directory: %w", err)
	}
	if insideGitWorktree(absolute) {
		return nil, errors.New("Grok runs directory must not be inside a Git worktree")
	}
	cfg.Storage.RunsDirectory = absolute
	return &Client{cfg: cfg, logger: logger.Named("grok")}, nil
}

func (c *Client) Check(_ context.Context) error {
	if _, err := c.findBinary(); err != nil {
		return &domain.SearchError{Code: "grok_not_found", Message: "Grok CLI was not found", Cause: err}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home for Grok authentication: %w", err)
	}
	authPath := filepath.Join(home, ".grok", "auth.json")
	info, err := os.Lstat(authPath)
	if err != nil {
		return &domain.SearchError{
			Code: "grok_not_authenticated", Message: "Grok authentication file was not found", Cause: err,
		}
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return &domain.SearchError{
			Code: "grok_not_authenticated", Message: "Grok authentication file is not a regular file",
		}
	}
	content, err := os.ReadFile(authPath)
	if err != nil {
		return fmt.Errorf("read Grok authentication file: %w", err)
	}
	var value any
	if json.Unmarshal(content, &value) != nil {
		return &domain.SearchError{
			Code: "grok_not_authenticated", Message: "Grok authentication file is not valid JSON",
		}
	}
	return nil
}

func (c *Client) Search(ctx context.Context, request ports.SearchRequest) (ports.SearchResult, error) {
	now := time.Now().UTC()
	if err := cleanupExpiredRuns(c.cfg.Storage.RunsDirectory, c.cfg.Grok.RetentionDays, now); err != nil {
		c.logger.Warn("failed to clean expired Grok runs", zap.Error(err))
	}
	binary, err := c.findBinary()
	if err != nil {
		return ports.SearchResult{}, &domain.SearchError{
			Code: "grok_not_found", Message: "Grok CLI was not found", Cause: err,
		}
	}
	runID := now.Format("20060102T150405Z") + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	runPath := filepath.Join(c.cfg.Storage.RunsDirectory, runID)
	if err := os.Mkdir(runPath, 0o700); err != nil {
		return ports.SearchResult{}, fmt.Errorf("create Grok run directory: %w", err)
	}
	if err := privateWrite(filepath.Join(runPath, runMarker), []byte("x-monitor Grok run v1\n")); err != nil {
		return ports.SearchResult{}, err
	}
	prompt := c.buildPrompt(request)
	if err := privateWrite(filepath.Join(runPath, "prompt.txt"), []byte(prompt)); err != nil {
		return ports.SearchResult{}, fmt.Errorf("write Grok prompt: %w", err)
	}
	sessionID := uuid.NewString()
	runManifest := manifest{
		RunID:       runID,
		CreatedAt:   now,
		Status:      "running",
		WindowStart: request.WindowStart.UTC(),
		WindowEnd:   request.WindowEnd.UTC(),
		Accounts:    request.Accounts,
		Model:       c.cfg.Grok.Model,
		SessionID:   sessionID,
	}
	if err := privateWriteJSON(filepath.Join(runPath, "manifest.json"), runManifest); err != nil {
		return ports.SearchResult{}, err
	}

	environment, cleanup, err := c.isolatedEnvironment(runPath)
	if err != nil {
		return ports.SearchResult{}, err
	}
	defer cleanup()

	command := []string{
		"--prompt-file", filepath.Join(runPath, "prompt.txt"),
		"--cwd", runPath,
		"--session-id", sessionID,
		"--tools", "x_search",
		"--deny", "MCPTool",
		"--always-approve",
		"--model", c.cfg.Grok.Model,
		"--output-format", "json",
		"--no-memory",
		"--no-subagents",
		"--no-plan",
		"--max-turns", strconv.Itoa(c.cfg.Grok.MaxTurns),
	}
	runContext, cancel := context.WithTimeout(ctx, c.cfg.Grok.Timeout)
	defer cancel()
	processResult := runProcess(runContext, binary, command, runPath, environment, c.cfg.Grok.MaxOutputSize)
	_ = privateWrite(filepath.Join(runPath, "stdout.json"), processResult.stdout)
	_ = privateWrite(filepath.Join(runPath, "stderr.txt"), processResult.stderr)

	answer := extractAnswer(processResult.stdout)
	if strings.TrimSpace(answer) == "" {
		code := "grok_execution_failed"
		message := "Grok did not return a usable answer"
		combined := string(processResult.stdout) + "\n" + string(processResult.stderr)
		if processResult.timedOut {
			code = "grok_timed_out"
			message = "Grok timed out before returning a usable answer"
		} else if authFailurePattern.MatchString(combined) {
			code = "grok_not_authenticated"
			message = "Grok authentication is required; run grok login"
		}
		runManifest.Status = "failed"
		runManifest.CompletedAt = time.Now().UTC()
		runManifest.ExitCode = processResult.exitCode
		runManifest.TimedOut = processResult.timedOut
		runManifest.ErrorCode = code
		runManifest.OutputCutoff = processResult.truncated
		_ = privateWriteJSON(filepath.Join(runPath, "manifest.json"), runManifest)
		return ports.SearchResult{}, &domain.SearchError{
			Code: code, Message: message, Path: runPath, Cause: processResult.err,
		}
	}
	resultPath := filepath.Join(runPath, "result.json")
	if err := privateWrite(resultPath, []byte(strings.TrimSpace(answer)+"\n")); err != nil {
		return ports.SearchResult{}, err
	}
	posts, err := parsePosts(answer, request)
	if err != nil {
		runManifest.Status = "failed"
		runManifest.CompletedAt = time.Now().UTC()
		runManifest.ExitCode = processResult.exitCode
		runManifest.ErrorCode = "grok_invalid_result"
		runManifest.ResultPath = resultPath
		_ = privateWriteJSON(filepath.Join(runPath, "manifest.json"), runManifest)
		return ports.SearchResult{}, &domain.SearchError{
			Code: "grok_invalid_result", Message: "Grok returned an invalid structured result", Path: runPath, Cause: err,
		}
	}
	partial := processResult.exitCode != 0 || processResult.timedOut || processResult.truncated
	runManifest.Status = "complete"
	if partial {
		runManifest.Status = "partial"
	}
	runManifest.CompletedAt = time.Now().UTC()
	runManifest.ExitCode = processResult.exitCode
	runManifest.TimedOut = processResult.timedOut
	runManifest.Partial = partial
	runManifest.ResultPath = resultPath
	runManifest.OutputCutoff = processResult.truncated
	_ = privateWriteJSON(filepath.Join(runPath, "manifest.json"), runManifest)
	return ports.SearchResult{
		RunID:      runID,
		Posts:      posts,
		ResultPath: resultPath,
		Partial:    partial,
	}, nil
}

func (c *Client) findBinary() (string, error) {
	candidates := make([]string, 0, 2)
	if c.cfg.Grok.Binary != "" {
		candidates = append(candidates, c.cfg.Grok.Binary)
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".grok", "bin", "grok"))
	}
	if discovered, err := exec.LookPath("grok"); err == nil {
		candidates = append(candidates, discovered)
	}
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return resolved, nil
		}
	}
	return "", errors.New("no executable Grok CLI candidate found")
}

func (c *Client) buildPrompt(request ports.SearchRequest) string {
	contentTypes := make([]string, 0, len(request.ContentTypes))
	for _, contentType := range request.ContentTypes {
		contentTypes = append(contentTypes, string(contentType))
	}
	if len(contentTypes) == 0 {
		contentTypes = []string{"original", "reply", "repost"}
	}
	return fmt.Sprintf(`Act as x-monitor's Grok X Search worker.

Task:
- Search only for public X/Twitter content authored by these accounts: %s
- Search window: %s through %s
- Platform: X
- Depth: %s
- Include only these content types: %s.
- Do not apply any keyword filter.
- Do not include content merely mentioning the accounts.
- Use direct X status URLs.
- Do not inspect local files, environment variables, credentials, repositories, or configuration.

Return JSON only, with no Markdown commentary, using exactly this schema:
{
  "schema_version": 1,
  "posts": [
    {
      "account": "@handle",
      "type": "original|reply|repost|unknown",
      "published_at": "RFC3339 timestamp or empty string",
      "status_url": "https://x.com/handle/status/numeric-id",
      "text": "original text when available",
      "summary_zh": "accurate concise Chinese summary",
      "uncertainties": ["explicit uncertainty, if any"]
    }
  ]
}

Rules:
- Deduplicate exact status URLs.
- Never invent timestamps, types, original text, URLs, or summaries.
- If a field cannot be confirmed, leave it empty or use "unknown" and explain it in uncertainties.
- Return {"schema_version":1,"posts":[]} when no matching content is found.
`, strings.Join(request.Accounts, ", "), request.WindowStart.UTC().Format(time.RFC3339),
		request.WindowEnd.UTC().Format(time.RFC3339), request.Depth, strings.Join(contentTypes, ", "))
}

func (c *Client) isolatedEnvironment(runPath string) ([]string, func(), error) {
	root, err := os.MkdirTemp("", "x-monitor-grok-")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create isolated Grok root: %w", err)
	}
	cleanupRoot := func() { _ = os.RemoveAll(root) }
	if err := os.Chmod(root, 0o700); err != nil {
		cleanupRoot()
		return nil, func() {}, err
	}
	home := filepath.Join(root, "home")
	grokHome := filepath.Join(home, ".grok")
	tmp := filepath.Join(home, "tmp")
	for _, path := range []string{home, grokHome, tmp, filepath.Join(home, ".config"), filepath.Join(home, ".cache")} {
		if err := ensurePrivateDirectory(path); err != nil {
			cleanupRoot()
			return nil, func() {}, err
		}
	}
	realHome, _ := os.UserHomeDir()
	realAuth := filepath.Join(realHome, ".grok", "auth.json")
	isolatedAuth := filepath.Join(grokHome, "auth.json")
	if err := copyValidJSON(realAuth, isolatedAuth); err != nil {
		cleanupRoot()
		return nil, func() {}, fmt.Errorf("copy Grok authentication: %w", err)
	}
	compatConfig := `[compat.cursor]
skills = false
rules = false
agents = false
mcps = false
hooks = false
sessions = false

[compat.claude]
skills = false
rules = false
agents = false
mcps = false
hooks = false
sessions = false

[compat.codex]
sessions = false
`
	if err := privateWrite(filepath.Join(grokHome, "config.toml"), []byte(compatConfig)); err != nil {
		cleanupRoot()
		return nil, func() {}, err
	}
	environment := allowedEnvironment()
	environment = append(environment,
		"HOME="+home,
		"GROK_HOME="+grokHome,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"TMPDIR="+tmp,
		"GROK_CURSOR_SKILLS_ENABLED=false",
		"GROK_CURSOR_RULES_ENABLED=false",
		"GROK_CURSOR_AGENTS_ENABLED=false",
		"GROK_CURSOR_MCPS_ENABLED=false",
		"GROK_CURSOR_HOOKS_ENABLED=false",
		"GROK_CLAUDE_SKILLS_ENABLED=false",
		"GROK_CLAUDE_RULES_ENABLED=false",
		"GROK_CLAUDE_AGENTS_ENABLED=false",
		"GROK_CLAUDE_MCPS_ENABLED=false",
		"GROK_CLAUDE_HOOKS_ENABLED=false",
	)
	cleanup := func() {
		if err := persistValidJSON(isolatedAuth, realAuth); err != nil {
			c.logger.Warn("failed to persist refreshed Grok authentication", zap.Error(err), zap.String("run_path", runPath))
		}
		cleanupRoot()
	}
	return environment, cleanup, nil
}

func allowedEnvironment() []string {
	allowed := map[string]struct{}{
		"PATH": {}, "LANG": {}, "LC_ALL": {}, "SSL_CERT_FILE": {}, "SSL_CERT_DIR": {},
		"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
		"http_proxy": {}, "https_proxy": {}, "all_proxy": {}, "no_proxy": {},
	}
	result := make([]string, 0, len(allowed))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[key]; ok {
			result = append(result, entry)
		}
	}
	return result
}

type processOutput struct {
	stdout    []byte
	stderr    []byte
	exitCode  int
	timedOut  bool
	truncated bool
	err       error
}

func runProcess(
	ctx context.Context,
	binary string,
	arguments []string,
	cwd string,
	environment []string,
	maxOutput int64,
) processOutput {
	command := exec.Command(binary, arguments...)
	command.Dir = cwd
	command.Env = environment
	configureProcess(command)
	stdout := newCappedBuffer(maxOutput)
	stderr := newCappedBuffer(maxOutput)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return processOutput{err: err, exitCode: -1}
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	var err error
	timedOut := false
	select {
	case err = <-wait:
	case <-ctx.Done():
		timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		_ = killProcessTree(command.Process)
		err = <-wait
	}
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return processOutput{
		stdout:    stdout.Bytes(),
		stderr:    stderr.Bytes(),
		exitCode:  exitCode,
		timedOut:  timedOut,
		truncated: stdout.Truncated() || stderr.Truncated(),
		err:       err,
	}
}

type cappedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	remaining int64
	truncated bool
}

func newCappedBuffer(limit int64) *cappedBuffer {
	return &cappedBuffer{remaining: limit}
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(value)
	if int64(len(value)) > b.remaining {
		value = value[:max(0, b.remaining)]
		b.truncated = true
	}
	if len(value) > 0 {
		_, _ = b.buffer.Write(value)
		b.remaining -= int64(len(value))
	}
	return originalLength, nil
}

func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buffer.Bytes())
}

func (b *cappedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func extractAnswer(stdout []byte) string {
	value := strings.TrimSpace(string(stdout))
	if value == "" {
		return ""
	}
	var envelope struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(stdout, &envelope) == nil && strings.TrimSpace(envelope.Text) != "" {
		return strings.TrimSpace(envelope.Text)
	}
	return value
}

var _ ports.Searcher = (*Client)(nil)
