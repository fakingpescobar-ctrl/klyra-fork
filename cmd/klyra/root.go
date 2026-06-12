package klyra

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"klyra/internal/version"
	"klyra/pkg/agent"
	"klyra/pkg/cockpit"
	appconfig "klyra/pkg/config"
	contextmgr "klyra/pkg/context"
	"klyra/pkg/instructions"
	"klyra/pkg/llm"
	"klyra/pkg/policy"
	"klyra/pkg/session"
	"klyra/pkg/skills"
	"klyra/pkg/tools"
	"klyra/pkg/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

type options struct {
	cwd                    string
	configPath             string
	profile                string
	provider               string
	model                  string
	baseURL                string
	fastModel              string
	editModel              string
	deepModel              string
	maxSteps               int
	maxMessages            int
	maxContext             int
	maxInstructions        int
	maxOutput              int
	reasoning              string
	store                  bool
	stream                 bool
	noStream               bool
	approval               string
	sandbox                string
	mode                   string
	contextFiles           []string
	sessionID              string
	contextCockpit         bool
	noContextCockpit       bool
	contextCockpitInject   bool
	noContextCockpitInject bool
	contextCockpitTokens   int
	contextCockpitMaxFiles int
	contextCockpitMaxCards int
	contextRetrieval       bool
	noContextRetrieval     bool
	contextRetrievalTokens int
	contextRetrievalChunks int
	contextEmbeddings      bool
	noContextEmbeddings    bool
	contextReranker        bool
	noContextReranker      bool
	contextRecipes         bool
	noContextRecipes       bool
	skills                 bool
	noSkills               bool
	negativeContext        bool
	noNegativeContext      bool
}

func Execute() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	opts := options{}
	root := &cobra.Command{
		Use:     "klyra",
		Short:   "Agentic coding CLI",
		Version: version.Version,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			setTerminalTitle(cmd.ErrOrStderr(), terminalTitleForProject(opts.cwd))
		},
	}

	root.PersistentFlags().StringVar(&opts.cwd, "cwd", ".", "workspace directory")
	root.PersistentFlags().StringVar(&opts.configPath, "config", "", "config file path")
	root.PersistentFlags().StringVar(&opts.profile, "profile", "", "config profile")
	root.PersistentFlags().StringVar(&opts.provider, "provider", "", "LLM provider: mock, openai, chat, ollama, anthropic, gemini")
	root.PersistentFlags().StringVar(&opts.model, "model", "", "model name; can also use provider-specific *_MODEL env vars")
	root.PersistentFlags().StringVar(&opts.baseURL, "base-url", "", "provider endpoint base URL override")
	root.PersistentFlags().StringVar(&opts.fastModel, "fast-model", "", "model for inspection/search tasks")
	root.PersistentFlags().StringVar(&opts.editModel, "edit-model", "", "model for coding/edit tasks")
	root.PersistentFlags().StringVar(&opts.deepModel, "deep-model", "", "model for architecture/security/deep tasks")
	root.PersistentFlags().IntVar(&opts.maxSteps, "max-steps", 0, "maximum agent loop steps")
	root.PersistentFlags().IntVar(&opts.maxMessages, "max-messages", 0, "maximum context messages")
	root.PersistentFlags().IntVar(&opts.maxContext, "max-context-tokens", 0, "estimated maximum context tokens")
	root.PersistentFlags().IntVar(&opts.maxInstructions, "max-instruction-bytes", 0, "maximum bytes of project instruction files to add to the system prompt")
	root.PersistentFlags().IntVar(&opts.maxOutput, "max-output-tokens", 0, "maximum model output tokens")
	root.PersistentFlags().StringVar(&opts.reasoning, "reasoning", "", "reasoning effort for providers that support it")
	root.PersistentFlags().BoolVar(&opts.store, "store", false, "allow provider-side response storage when supported")
	root.PersistentFlags().BoolVar(&opts.stream, "stream", false, "stream model output when the provider supports it")
	root.PersistentFlags().BoolVar(&opts.noStream, "no-stream", false, "disable model output streaming")
	root.PersistentFlags().StringVar(&opts.approval, "approval", "", "tool approval mode: auto, ask, always, never")
	root.PersistentFlags().StringVar(&opts.sandbox, "sandbox", "", "sandbox profile: read-only, workspace-write, danger-full-access")
	root.PersistentFlags().StringVar(&opts.mode, "mode", "", "agent mode: plan, inspect, edit, repair, refactor")
	root.PersistentFlags().StringSliceVar(&opts.contextFiles, "context-file", nil, "file allowed in edit/refactor context cart; repeatable")
	root.PersistentFlags().StringVar(&opts.sessionID, "session", "", "session id for persistent conversations")
	root.PersistentFlags().BoolVar(&opts.contextCockpit, "context-cockpit", false, "enable context cockpit fact cards")
	root.PersistentFlags().BoolVar(&opts.noContextCockpit, "no-context-cockpit", false, "disable context cockpit fact cards")
	root.PersistentFlags().BoolVar(&opts.contextCockpitInject, "context-cockpit-inject", false, "inject context cockpit fact cards into model context")
	root.PersistentFlags().BoolVar(&opts.noContextCockpitInject, "no-context-cockpit-inject", false, "show context cockpit without injecting it into model context")
	root.PersistentFlags().IntVar(&opts.contextCockpitTokens, "context-cockpit-tokens", 0, "context cockpit token budget")
	root.PersistentFlags().IntVar(&opts.contextCockpitMaxFiles, "context-cockpit-files", 0, "maximum files ranked in context cockpit repo map")
	root.PersistentFlags().IntVar(&opts.contextCockpitMaxCards, "context-cockpit-cards", 0, "maximum context cockpit cards")
	root.PersistentFlags().BoolVar(&opts.contextRetrieval, "context-retrieval", false, "enable BM25/AST retrieval cart cards")
	root.PersistentFlags().BoolVar(&opts.noContextRetrieval, "no-context-retrieval", false, "disable BM25/AST retrieval cart cards")
	root.PersistentFlags().IntVar(&opts.contextRetrievalTokens, "context-retrieval-tokens", 0, "context retrieval cart token budget")
	root.PersistentFlags().IntVar(&opts.contextRetrievalChunks, "context-retrieval-chunks", 0, "maximum context retrieval chunks")
	root.PersistentFlags().BoolVar(&opts.contextEmbeddings, "context-embeddings", false, "enable semantic retrieval when configured")
	root.PersistentFlags().BoolVar(&opts.noContextEmbeddings, "no-context-embeddings", false, "disable semantic retrieval")
	root.PersistentFlags().BoolVar(&opts.contextReranker, "context-reranker", false, "enable reranker when configured")
	root.PersistentFlags().BoolVar(&opts.noContextReranker, "no-context-reranker", false, "disable reranker")
	root.PersistentFlags().BoolVar(&opts.contextRecipes, "context-recipes", false, "enable scoped context recipes")
	root.PersistentFlags().BoolVar(&opts.noContextRecipes, "no-context-recipes", false, "disable scoped context recipes")
	root.PersistentFlags().BoolVar(&opts.skills, "skills", false, "enable matched project skills")
	root.PersistentFlags().BoolVar(&opts.noSkills, "no-skills", false, "disable matched project skills")
	root.PersistentFlags().BoolVar(&opts.negativeContext, "negative-context", false, "enable negative context deny-list cards")
	root.PersistentFlags().BoolVar(&opts.noNegativeContext, "no-negative-context", false, "disable negative context deny-list cards")

	var jsonOutput bool
	var runTimeout string
	var runParallel bool
	var runDryRun bool
	var runRetry int
	var runWatch bool
	var runWatchGlob string
	var runWatchInterval string
	runCmd := &cobra.Command{
		Use:   "run [task]",
		Short: "Run an agent task",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeCfg, err := effectiveConfig(cmd, opts)
			if err != nil {
				return err
			}
			provider, model, err := buildProviderFromConfig(runtimeCfg)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if runTimeout != "" {
				dur, parseErr := time.ParseDuration(runTimeout)
				if parseErr != nil {
					return fmt.Errorf("--timeout: %w", parseErr)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, dur)
				defer cancel()
			}
			toolRegistry, err := buildToolRegistry(ctx, runtimeCfg)
			if err != nil {
				return err
			}
			baseCfg := buildBaseAgentConfig(runtimeCfg, opts.cwd, model, provider, toolRegistry)
			baseCfg.Input = os.Stdin
			baseCfg.Output = cmd.OutOrStdout()

			// --dry-run: intercept all tool calls, print what would run, don't execute
			if runDryRun {
				var dryRunTools []string
				baseCfg.Approver = func(req agent.ApprovalRequest) (bool, error) {
					entry := req.Tool
					if len(req.Args) > 0 {
						if data, merr := json.Marshal(req.Args); merr == nil {
							entry += " " + string(data)
						}
					}
					dryRunTools = append(dryRunTools, entry)
					return false, fmt.Errorf("dry-run: tool call blocked")
				}
				out := cmd.OutOrStdout()
				fmt.Fprintln(out, "[dry-run] agent would execute the following tools:")
				runner, err := agent.New(baseCfg)
				if err != nil {
					return err
				}
				task := strings.Join(args, " ")
				runner.RunConversation(ctx, nil, task) //nolint:errcheck
				if len(dryRunTools) == 0 {
					fmt.Fprintln(out, "  (no tool calls planned)")
				}
				for i, t := range dryRunTools {
					fmt.Fprintf(out, "  %d. %s\n", i+1, t)
				}
				return nil
			}

			// --parallel: run each arg as an independent sub_agent concurrently
			if runParallel && len(args) > 1 {
				factory := agent.DefaultSubAgentFactory(baseCfg)
				results := make([]string, len(args))
				errs := make([]error, len(args))
				var wg sync.WaitGroup
				for i, task := range args {
					wg.Add(1)
					go func(idx int, t string) {
						defer wg.Done()
						results[idx], errs[idx] = factory(ctx, t, runtimeCfg.Mode, runtimeCfg.ContextFiles)
					}(i, task)
				}
				wg.Wait()
				for i, task := range args {
					fmt.Fprintf(cmd.OutOrStdout(), "\n=== [%d/%d] %s ===\n%s\n", i+1, len(args), task, results[i])
					if errs[i] != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "error: %v\n", errs[i])
					}
				}
				return nil
			}

			runner, err := agent.New(baseCfg)
			if err != nil {
				return err
			}
			task := strings.Join(args, " ")

			runWithRetry := func(msgs []llm.Message) (agent.RunResult, error) {
				maxAttempts := 1
				if runRetry > 0 {
					maxAttempts = runRetry + 1
				}
				var lastErr error
				var lastResult agent.RunResult
				for attempt := 0; attempt < maxAttempts; attempt++ {
					if attempt > 0 {
						backoff := time.Duration(1<<uint(attempt-1)) * time.Second
						fmt.Fprintf(cmd.OutOrStdout(), "retry %d/%d after %s: %v\n", attempt, runRetry, backoff, lastErr)
						select {
						case <-ctx.Done():
							return lastResult, ctx.Err()
						case <-time.After(backoff):
						}
					}
					lastResult, lastErr = runner.RunConversation(ctx, msgs, task)
					if lastErr == nil {
						return lastResult, nil
					}
				}
				return lastResult, lastErr
			}

			runOnce := func() error {
				if strings.TrimSpace(opts.sessionID) == "" {
					result, runErr := runWithRetry(nil)
					if jsonOutput {
						return printJSONResult(cmd.OutOrStdout(), result)
					}
					return runErr
				}
				store, err := session.NewStore(opts.cwd)
				if err != nil {
					return err
				}
				saved, err := store.LoadOrCreate(opts.sessionID, opts.cwd)
				if err != nil {
					return err
				}
				result, err := runWithRetry(saved.Messages)
				saved.Messages = result.Messages
				if saveErr := store.Save(saved); saveErr != nil {
					return saveErr
				}
				printContextDebug(cmd.OutOrStdout(), result.ContextDebug)
				fmt.Fprintf(cmd.OutOrStdout(), "session: %s\n", saved.ID)
				if jsonOutput {
					return printJSONResult(cmd.OutOrStdout(), result)
				}
				return err
			}

			if !runWatch {
				return runOnce()
			}

			// --watch mode: run once, then re-run on file changes
			interval := 500 * time.Millisecond
			if runWatchInterval != "" {
				if d, parseErr := time.ParseDuration(runWatchInterval); parseErr == nil {
					interval = d
				}
			}
			collectMTimes := func() map[string]time.Time {
				mtimes := map[string]time.Time{}
				filepath.WalkDir(opts.cwd, func(path string, d os.DirEntry, walkErr error) error { //nolint:errcheck
					if walkErr != nil || d.IsDir() {
						return nil
					}
					rel, _ := filepath.Rel(opts.cwd, path)
					rel = filepath.ToSlash(rel)
					if matched, _ := filepath.Match(runWatchGlob, filepath.Base(rel)); !matched {
						if runWatchGlob != "**/*" {
							return nil
						}
					}
					if info, err := d.Info(); err == nil {
						mtimes[rel] = info.ModTime()
					}
					return nil
				})
				return mtimes
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[watch] watching %s every %s\n", runWatchGlob, interval)
			if err := runOnce(); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "[watch] error: %v\n", err)
			}
			prev := collectMTimes()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(interval):
				}
				curr := collectMTimes()
				changed := false
				for p, mt := range curr {
					if prev[p] != mt {
						changed = true
						fmt.Fprintf(cmd.OutOrStdout(), "[watch] changed: %s\n", p)
						break
					}
				}
				if !changed {
					for p := range prev {
						if _, ok := curr[p]; !ok {
							changed = true
							fmt.Fprintf(cmd.OutOrStdout(), "[watch] deleted: %s\n", p)
							break
						}
					}
				}
				if changed {
					prev = curr
					fmt.Fprintf(cmd.OutOrStdout(), "[watch] re-running...\n")
					if err := runOnce(); err != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "[watch] error: %v\n", err)
					}
				}
			}
		},
	}
	runCmd.Flags().BoolVar(&jsonOutput, "json", false, "output result as JSON {result, usage}")
	runCmd.Flags().StringVar(&runTimeout, "timeout", "", "max wall-clock time (e.g. 5m, 30s); cancels via context")
	runCmd.Flags().BoolVar(&runParallel, "parallel", false, "run each positional arg as a parallel sub_agent")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "show what tool calls the agent would make without executing them")
	runCmd.Flags().IntVar(&runRetry, "retry", 0, "number of retries on provider error (exponential backoff: 1s, 2s, 4s, …)")
	runCmd.Flags().BoolVar(&runWatch, "watch", false, "re-run agent when files matching --watch-glob change")
	runCmd.Flags().StringVar(&runWatchGlob, "watch-glob", "**/*", "glob pattern of files to watch (default: all files)")
	runCmd.Flags().StringVar(&runWatchInterval, "watch-interval", "500ms", "polling interval for file change detection")

	root.AddCommand(runCmd)
	root.AddCommand(newChatCommand(&opts))
	root.AddCommand(newTUICommand(&opts))
	root.AddCommand(newStatusCommand(&opts))
	root.AddCommand(newCheckpointCommand(&opts))
	root.AddCommand(newDiffCommand(&opts))
	root.AddCommand(newPolicyCommand())
	root.AddCommand(newToolsCommand(&opts))
	root.AddCommand(newInstructionsCommand(&opts))
	root.AddCommand(newSkillsCommand(&opts))
	root.AddCommand(newDoctorCommand(&opts))
	root.AddCommand(newConfigCommand(&opts))
	root.AddCommand(newSessionsCommand(&opts))
	root.AddCommand(newInitCommand(&opts))
	return root
}

func newTUIApprover(p **tea.Program) agent.ApprovalFunc {
	return func(req agent.ApprovalRequest) (bool, error) {
		if p == nil || *p == nil {
			return false, fmt.Errorf("approval UI is not ready")
		}
		reply := make(chan bool, 1)
		(*p).Send(tui.ApprovalRequestMsg{
			Tool:   req.Tool,
			Risk:   req.Risk,
			Reason: req.Reason,
			Args:   req.Args,
			Reply:  reply,
		})
		return <-reply, nil
	}
}

type tuiStreamDispatcher struct {
	program func() *tea.Program
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once

	mu           sync.Mutex
	text         strings.Builder
	reasoning    strings.Builder
	toolStreams  []tui.ToolStreamMsg
	toolProgress []tui.ToolProgressMsg
}

func newTUIStreamDispatcher(program func() *tea.Program) *tuiStreamDispatcher {
	dispatcher := &tuiStreamDispatcher{
		program: program,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go dispatcher.run()
	return dispatcher
}

func (d *tuiStreamDispatcher) SendText(text string) {
	if text == "" {
		return
	}
	d.mu.Lock()
	d.text.WriteString(text)
	d.mu.Unlock()
}

func (d *tuiStreamDispatcher) SendReasoning(text string) {
	if text == "" {
		return
	}
	d.mu.Lock()
	d.reasoning.WriteString(text)
	d.mu.Unlock()
}

func (d *tuiStreamDispatcher) SendToolStream(msg tui.ToolStreamMsg) {
	d.mu.Lock()
	d.toolStreams = append(d.toolStreams, msg)
	d.mu.Unlock()
}

func (d *tuiStreamDispatcher) SendToolProgress(msg tui.ToolProgressMsg) {
	d.mu.Lock()
	d.toolProgress = append(d.toolProgress, msg)
	d.mu.Unlock()
}

func (d *tuiStreamDispatcher) Close() {
	d.once.Do(func() { close(d.done) })
	select {
	case <-d.stopped:
	case <-time.After(500 * time.Millisecond):
	}
}

func (d *tuiStreamDispatcher) run() {
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()
	defer close(d.stopped)
	for {
		select {
		case <-ticker.C:
			d.flush()
		case <-d.done:
			d.flush()
			return
		}
	}
}

func (d *tuiStreamDispatcher) flush() {
	text, reasoning, toolStreams, toolProgress := d.drain()
	if text == "" && reasoning == "" && len(toolStreams) == 0 && len(toolProgress) == 0 {
		return
	}
	if d.program == nil {
		return
	}
	program := d.program()
	if program == nil {
		return
	}
	if reasoning != "" {
		program.Send(tui.ReasoningMsg(reasoning))
	}
	if text != "" {
		program.Send(tui.StreamMsg(text))
	}
	for _, msg := range toolStreams {
		program.Send(msg)
	}
	for _, msg := range toolProgress {
		program.Send(msg)
	}
}

func (d *tuiStreamDispatcher) drain() (string, string, []tui.ToolStreamMsg, []tui.ToolProgressMsg) {
	d.mu.Lock()
	defer d.mu.Unlock()
	text := d.text.String()
	reasoning := d.reasoning.String()
	toolStreams := append([]tui.ToolStreamMsg(nil), d.toolStreams...)
	toolProgress := append([]tui.ToolProgressMsg(nil), d.toolProgress...)
	d.text.Reset()
	d.reasoning.Reset()
	d.toolStreams = nil
	d.toolProgress = nil
	return text, reasoning, toolStreams, toolProgress
}

func newTUICommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the Bubble Tea terminal UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.LoadOrCreate(opts.sessionID, opts.cwd)
			if err != nil {
				return err
			}
			tuiCommands := []tui.CommandDef{
				{Name: "/help", Description: "Show available commands"},
				{Name: "/clear", Description: "Clear chat history"},
				{Name: "/status", Description: "Show workspace status"},
				{Name: "/compact", Description: "Compact chat history to reduce tokens"},
				{Name: "/settings", Description: "Open settings form"},
				{Name: "/features", Description: "Toggle features on/off"},
				{Name: "/provider", Description: "Set provider: mock/openai/chat/ollama/anthropic/gemini"},
				{Name: "/model", Description: "Set the active model name"},
				{Name: "/endpoint", Description: "Set provider endpoint base URL"},
				{Name: "/reasoning", Description: "Set reasoning effort: minimal/low/medium/high/xhigh"},
				{Name: "/limits", Description: "Set token/step budgets"},
				{Name: "/approval", Description: "Set approval mode: auto/ask/always/never"},
				{Name: "/sandbox", Description: "Set sandbox: read-only/workspace-write/danger-full-access"},
				{Name: "/mode", Description: "Set mode: plan/inspect/edit/repair/refactor"},
				{Name: "/cart", Description: "Show, add, remove, or clear context cart files"},
				{Name: "/cart add", Description: "Add files to context cart: /cart add <file...>"},
				{Name: "/cart remove", Description: "Remove files from context cart: /cart remove <file...>"},
				{Name: "/cart clear", Description: "Clear all files from context cart"},
				{Name: "/context", Description: "Show context cockpit fact cards"},
				{Name: "/attach", Description: "Attach an image to the next model request"},
				{Name: "/attachments", Description: "Show pending image attachments"},
				{Name: "/skills", Description: "Show matched project skills"},
				{Name: "/doctor", Description: "Check local runtime support"},
				{Name: "/tools", Description: "Toggle agent tools on/off"},
				{Name: "/instructions", Description: "Show project instruction files"},
				{Name: "/undo", Description: "Restore the most recent workspace checkpoint"},
				{Name: "/sessions", Description: "List saved workspace sessions"},
				{Name: "/sessions rename", Description: "Rename a session: /sessions rename <old> <new>"},
				{Name: "/sessions delete", Description: "Delete a session by id"},
				{Name: "/sessions prune", Description: "Delete sessions older than N days: /sessions prune --days=30"},
				{Name: "/checkpoint", Description: "Open checkpoint actions"},
				{Name: "/checkpoint list", Description: "List workspace checkpoints"},
				{Name: "/checkpoint create", Description: "Create a workspace checkpoint"},
				{Name: "/checkpoint restore", Description: "Restore files from a checkpoint"},
				{Name: "/diff", Description: "Open diff actions"},
				{Name: "/diff preview", Description: "Preview a patch file"},
				{Name: "/diff apply", Description: "Apply a patch file (requires --yes)"},
				{Name: "/policy check", Description: "Classify a shell command by risk"},
				{Name: "/config", Description: "Open config actions"},
				{Name: "/config show", Description: "Print effective configuration"},
				{Name: "/config init", Description: "Write default config file"},
				{Name: "/save", Description: "Manually save the session state"},
				{Name: "/session", Description: "Switch to a saved session by id"},
				{Name: "/exit", Description: "Exit the TUI"},
				{Name: "/quit", Description: "Exit the TUI (alias)"},
			}

			var p *tea.Program
			type activeRun struct {
				cancel context.CancelFunc
			}
			var activeMu sync.Mutex
			var active *activeRun
			pendingAttachments := []llm.Attachment{}

			handler := func(input string) (string, error) {
				trimmed := strings.TrimSpace(input)
				if strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "/exit") && !strings.HasPrefix(trimmed, "/quit") && !strings.HasPrefix(trimmed, "/clear") {
					args := strings.Fields(trimmed)
					cmdName := args[0]

					switch cmdName {
					case "/help":
						var helpOut strings.Builder
						helpOut.WriteString("Available commands:\n")
						for _, c := range tuiCommands {
							helpOut.WriteString(fmt.Sprintf("  %-20s %s\n", c.Name, c.Description))
						}
						return strings.TrimSpace(helpOut.String()), nil
					case "/status":
						return tuiStatus(opts.cwd)
					case "/save":
						if err := store.Save(saved); err != nil {
							return "", err
						}
						return fmt.Sprintf("Session Saved\n\n- ID: `%s`", saved.ID), nil
					case "/session":
						if len(args) < 2 {
							return "usage: /session <id>", nil
						}
						next, err := store.Load(args[1])
						if err != nil {
							return "", err
						}
						saved = next
						opts.sessionID = saved.ID
						if p != nil {
							p.Send(tui.SessionLoadedMsg{
								SessionID: saved.ID,
								Lines:     tuiLinesFromMessages(saved.Messages),
							})
						}
						return fmt.Sprintf("Session Switched\n\n- ID: `%s`\n- Messages: `%d`\n- Updated: `%s`",
							saved.ID, len(saved.Messages), saved.UpdatedAt.Format("2006-01-02 15:04:05")), nil
					case "/compact":
						compacted, stats := contextmgr.CompactMessages(saved.Messages, runtimeCfg.MaxContext, runtimeCfg.MaxMessages/2)
						saved.Messages = compacted
						if err := store.Save(saved); err != nil {
							return "", err
						}
						return fmt.Sprintf("Session Compacted\n\n- Messages: `%d` ➔ `%d`\n- Estimated tokens: `%d` ➔ `%d`",
							stats.OriginalMessages, stats.PackedMessages, stats.OriginalTokens, stats.PackedTokens), nil
					case "/settings":
						_ = runtimeCfg.Save(opts.configPath)
						return formatTUISettings(runtimeCfg, pendingAttachments), nil
					case "/set":
						if err := applyTUISet(&runtimeCfg, args[1:]); err != nil {
							return "", err
						}
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("settings", fmt.Sprintf("%d value(s)", len(args)-1)), nil
					case "/provider":
						if len(args) < 2 {
							return "usage: /provider mock|openai|chat|ollama|anthropic|gemini", nil
						}
						runtimeCfg.Provider = args[1]
						runtimeCfg.Model = ""
						if runtimeCfg.Provider == "mock" {
							runtimeCfg.Model = "mock-agent"
						}
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("provider", runtimeCfg.Provider), nil
					case "/model":
						if len(args) < 2 {
							return "usage: /model <model-name>", nil
						}
						runtimeCfg.Model = strings.Join(args[1:], " ")
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("model", runtimeCfg.Model), nil
					case "/endpoint":
						if len(args) < 2 {
							return "usage: /endpoint <base-url>", nil
						}
						setProviderBaseURL(&runtimeCfg, runtimeCfg.Provider, strings.Join(args[1:], " "))
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("endpoint", providerBaseURL(runtimeCfg, runtimeCfg.Provider)), nil
					case "/reasoning":
						if len(args) < 2 {
							return "usage: /reasoning minimal|low|medium|high|xhigh", nil
						}
						runtimeCfg.Reasoning = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("reasoning", runtimeCfg.Reasoning), nil
					case "/limits":
						if len(args) == 1 {
							return "Limits Usage\n\n`Format:` `/limits [context|output|steps|messages|instructions] <value>`\n\n*Example:* `/limits context 32000`", nil
						}
						if err := applyTUILimit(&runtimeCfg, args[1:]); err != nil {
							return "", err
						}
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("limit "+args[1], args[2]), nil
					case "/approval":
						if len(args) < 2 {
							return "usage: /approval auto|ask|always|never", nil
						}
						runtimeCfg.ApprovalMode = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("approval", runtimeCfg.ApprovalMode), nil
					case "/sandbox":
						if len(args) < 2 {
							return "usage: /sandbox read-only|workspace-write|danger-full-access", nil
						}
						runtimeCfg.Sandbox = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("sandbox", runtimeCfg.Sandbox), nil
					case "/mode":
						if len(args) < 2 {
							return "usage: /mode plan|inspect|edit|repair|refactor", nil
						}
						runtimeCfg.Mode = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("mode", runtimeCfg.Mode), nil
					case "/cart":
						if len(args) >= 3 && args[1] == "add" {
							runtimeCfg.ContextFiles = append(runtimeCfg.ContextFiles, args[2:]...)
						} else if len(args) >= 3 && args[1] == "remove" {
							toRemove := map[string]bool{}
							for _, f := range args[2:] {
								toRemove[f] = true
							}
							filtered := runtimeCfg.ContextFiles[:0]
							for _, f := range runtimeCfg.ContextFiles {
								if !toRemove[f] {
									filtered = append(filtered, f)
								}
							}
							runtimeCfg.ContextFiles = filtered
						} else if len(args) >= 2 && args[1] == "clear" {
							runtimeCfg.ContextFiles = nil
						}
						return formatContextCart(runtimeCfg.ContextFiles), nil
					case "/context":
						return formatContextCockpit(runtimeCfg, opts.cwd, strings.TrimSpace(strings.TrimPrefix(trimmed, "/context")))
					case "/attach":
						if len(args) < 2 {
							return "usage: /attach path/to/image.png", nil
						}
						attachment, err := loadImageAttachment(opts.cwd, strings.Join(args[1:], " "))
						if err != nil {
							return "", err
						}
						pendingAttachments = append(pendingAttachments, attachment)
						return fmt.Sprintf("Image Attached\n\n- Name: `%s`\n- Type: `%s`\n- Size: `%d` bytes\n\n*Attachment will be sent with the next request.*", attachment.Name, attachment.MIMEType, len(attachment.Data)), nil
					case "/attachments":
						return formatAttachments(pendingAttachments), nil
					case "/diff":
						if len(args) >= 2 && args[1] == "apply" {
							hasYes := false
							for _, a := range args {
								if a == "--yes" {
									hasYes = true
									break
								}
							}
							if !hasYes {
								return "diff apply requires --yes in the TUI to confirm.", nil
							}
						}
						fallthrough
					case "/undo":
						listResult, listErr := (tools.WorkspaceCheckpointList{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{}})
						if listErr != nil {
							return "", listErr
						}
						var checkpoints []string
						for _, line := range strings.Split(listResult.Output, "\n") {
							id := strings.TrimSpace(line)
							if id != "" && id != "no checkpoints" {
								checkpoints = append(checkpoints, id)
							}
						}
						if len(checkpoints) == 0 {
							return "Undo\n\n*No checkpoints to restore.*", nil
						}
						latest := checkpoints[len(checkpoints)-1]
						restoreResult, restoreErr := (tools.WorkspaceRestore{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{"id": latest}})
						if restoreErr != nil {
							return "", restoreErr
						}
						return fmt.Sprintf("Undo\n\nRestored checkpoint: `%s`\n\n%s", latest, restoreResult.Output), nil
					case "/sessions":
						if len(args) >= 4 && args[1] == "rename" {
							sessionStore, storeErr := session.NewStore(opts.cwd)
							if storeErr != nil {
								return "", storeErr
							}
							if renameErr := sessionStore.Rename(args[2], args[3]); renameErr != nil {
								return "", renameErr
							}
							return fmt.Sprintf("session renamed: `%s` → `%s`", args[2], args[3]), nil
						}
						if len(args) >= 3 && args[1] == "delete" {
							sessionStore, storeErr := session.NewStore(opts.cwd)
							if storeErr != nil {
								return "", storeErr
							}
							if delErr := sessionStore.Delete(args[2]); delErr != nil {
								return "", delErr
							}
							return fmt.Sprintf("session deleted: `%s`", args[2]), nil
						}
						if len(args) >= 2 && args[1] == "prune" {
							days := 30
							for _, a := range args[2:] {
								if n, parseErr := strconv.Atoi(strings.TrimPrefix(a, "--days=")); parseErr == nil {
									days = n
								}
							}
							sessionStore, storeErr := session.NewStore(opts.cwd)
							if storeErr != nil {
								return "", storeErr
							}
							count, pruneErr := sessionStore.Prune(time.Duration(days) * 24 * time.Hour)
							if pruneErr != nil {
								return "", pruneErr
							}
							return fmt.Sprintf("pruned %d session(s) older than %d days", count, days), nil
						}
						fallthrough
					case "/doctor", "/tools", "/instructions", "/skills", "/checkpoint", "/policy", "/config":
						cliCmdName := strings.TrimPrefix(cmdName, "/")
						var out strings.Builder
						subCmd := newRootCommand()
						subCmd.SetArgs(append([]string{cliCmdName}, args[1:]...))
						subCmd.SetOut(&out)
						subCmd.SetErr(&out)
						err := subCmd.Execute()
						return strings.TrimSpace(out.String()), err
					}
				}
				provider, model, err := buildProviderFromConfig(runtimeCfg)
				if err != nil {
					return "", err
				}
				toolRegistry, err := buildToolRegistry(context.Background(), runtimeCfg)
				if err != nil {
					return "", err
				}
				var output strings.Builder
				sawStream := false
				streamDispatcher := newTUIStreamDispatcher(func() *tea.Program { return p })
				tuiCfg := buildBaseAgentConfig(runtimeCfg, opts.cwd, model, provider, toolRegistry)
				tuiCfg.Input = os.Stdin
				tuiCfg.Output = &output
				tuiCfg.Approver = newTUIApprover(&p)
				tuiCfg.StreamHandler = func(event llm.StreamEvent) error {
					if p == nil {
						return nil
					}
					if event.ToolName != "" || event.ToolArgumentsDelta != "" {
						sawStream = true
						streamDispatcher.SendToolStream(tui.ToolStreamMsg{
							Index:     event.ToolCallIndex,
							ID:        event.ToolCallID,
							Name:      event.ToolName,
							Arguments: event.ToolArgumentsDelta,
						})
					}
					if event.Delta != "" {
						sawStream = true
						streamDispatcher.SendText(event.Delta)
					}
					return nil
				}
				tuiCfg.ReasoningHandler = func(text string) error {
					if text == "" || p == nil {
						return nil
					}
					streamDispatcher.SendReasoning(text)
					return nil
				}
				tuiCfg.ToolProgress = func(event agent.ToolProgressEvent) error {
					if p == nil {
						return nil
					}
					sawStream = true
					streamDispatcher.SendToolProgress(tui.ToolProgressMsg{
						Phase:  event.Phase,
						Tool:   event.Tool,
						ID:     event.ID,
						Args:   event.Args,
						Output: event.Output,
						Error:  event.Error,
					})
					return nil
				}
				runnerWithOutput, err := agent.New(tuiCfg)
				if err != nil {
					streamDispatcher.Close()
					return "", err
				}
				attachments := pendingAttachments
				pendingAttachments = nil
				runCtx, cancel := context.WithCancel(context.Background())
				runState := &activeRun{cancel: cancel}
				activeMu.Lock()
				active = runState
				activeMu.Unlock()
				defer func() {
					activeMu.Lock()
					if active == runState {
						active = nil
					}
					activeMu.Unlock()
					cancel()
				}()

				startTime := time.Now()
				result, err := runnerWithOutput.RunConversationWithAttachments(runCtx, saved.Messages, input, attachments)
				streamDispatcher.Close()
				elapsed := time.Since(startTime)

				// Attach duration and usage to the last assistant message in result.Messages
				for i := len(result.Messages) - 1; i >= 0; i-- {
					if result.Messages[i].Role == llm.RoleAssistant {
						result.Messages[i].DurationSeconds = elapsed.Seconds()
						u := result.Usage
						result.Messages[i].Usage = &u
						break
					}
				}

				saved.Messages = result.Messages
				if saveErr := store.Save(saved); saveErr != nil {
					return "", saveErr
				}
				captured := strings.TrimSpace(output.String())
				debug := formatContextDebug(result.ContextDebug)
				usageStr := fmt.Sprintf("[TokenUsage] input=%d cached=%d output=%d reasoning=%d total=%d",
					result.Usage.InputTokens,
					result.Usage.CachedTokens,
					result.Usage.OutputTokens,
					result.Usage.ReasoningTokens,
					result.Usage.TotalTokens,
				)
				if sawStream {
					return strings.TrimSpace(debug + "\n\n" + usageStr), err
				}
				if strings.Contains(captured, "tool:") || strings.Contains(captured, "tool error:") || strings.Contains(captured, "usage:") {
					return strings.TrimSpace(output.String() + "\n\n" + debug + "\n\n" + usageStr), err
				}
				return strings.TrimSpace(result.Final + "\n\n" + debug + "\n\n" + usageStr), err
			}

			allToolsRegistry, _ := buildToolRegistry(context.Background(), runtimeCfg)
			var allToolsList []string
			if allToolsRegistry != nil {
				for _, spec := range allToolsRegistry.Specs() {
					allToolsList = append(allToolsList, spec.Name)
				}
			}

			tuiModel := tui.New(tui.Config{
				CWD:                    opts.cwd,
				Title:                  "Klyra",
				SessionID:              saved.ID,
				Provider:               runtimeCfg.Provider,
				Model:                  runtimeCfg.Model,
				BaseURL:                providerBaseURL(runtimeCfg, runtimeCfg.Provider),
				BaseURLs:               runtimeCfg.BaseURLs,
				Reasoning:              runtimeCfg.Reasoning,
				Stream:                 runtimeCfg.Stream,
				Sandbox:                runtimeCfg.Sandbox,
				Approval:               runtimeCfg.ApprovalMode,
				Mode:                   runtimeCfg.Mode,
				StoreResponses:         runtimeCfg.StoreResponses,
				CartCount:              len(runtimeCfg.ContextFiles),
				MaxContext:             runtimeCfg.MaxContext,
				MaxOutput:              runtimeCfg.MaxOutput,
				MaxSteps:               runtimeCfg.MaxSteps,
				MaxMessages:            runtimeCfg.MaxMessages,
				MaxInstructions:        runtimeCfg.MaxInstructions,
				ContextCockpit:         runtimeCfg.ContextCockpit,
				ContextCockpitInject:   runtimeCfg.ContextCockpitInject,
				ContextCockpitTokens:   runtimeCfg.ContextCockpitTokens,
				ContextCockpitMaxFiles: runtimeCfg.ContextCockpitMaxFiles,
				ContextCockpitMaxCards: runtimeCfg.ContextCockpitMaxCards,
				ContextRetrieval:       runtimeCfg.ContextRetrieval,
				ContextRetrievalTokens: runtimeCfg.ContextRetrievalTokens,
				ContextRetrievalChunks: runtimeCfg.ContextRetrievalChunks,
				ContextEmbeddings:      runtimeCfg.ContextEmbeddings,
				ContextReranker:        runtimeCfg.ContextReranker,
				ContextCockpitDiff:     runtimeCfg.ContextCockpitDiff,
				ContextRecipes:         runtimeCfg.ContextRecipes,
				NegativeContext:        runtimeCfg.NegativeContext,
				Skills:                 runtimeCfg.Skills,
				FastModel:              runtimeCfg.ModelRoutes["fast"],
				EditModel:              runtimeCfg.ModelRoutes["edit"],
				DeepModel:              runtimeCfg.ModelRoutes["deep"],
				AllTools:               allToolsList,
				DisabledTools:          runtimeCfg.DisabledTools,
				Handler:                handler,
				Interrupt: func() bool {
					activeMu.Lock()
					runState := active
					activeMu.Unlock()
					if runState == nil || runState.cancel == nil {
						return false
					}
					runState.cancel()
					return true
				},
				PickerProvider: func(field string) (tui.PickerModal, error) {
					switch field {
					case "session":
						return tuiSessionPicker(store, saved.ID)
					case "checkpoint_restore":
						return tuiCheckpointRestorePicker(opts.cwd)
					default:
						return tui.PickerModal{}, fmt.Errorf("unknown picker %q", field)
					}
				},
				Commands:     tuiCommands,
				InitialLines: tuiLinesFromMessages(saved.Messages),
				SidebarFiles: tuiSidebarFiles(opts.cwd),
				SidebarDiff:  tuiSidebarDiff(opts.cwd),
			})
			p = tea.NewProgram(tuiModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
			_, err = p.Run()
			return err
		},
	}
}

func tuiStatus(cwd string) (string, error) {
	status, err := (tools.GitStatus{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{}})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status.Output) == "" {
		return "clean", nil
	}
	return status.Output, nil
}

func tuiSidebarFiles(cwd string) []string {
	result, err := (tools.ListFiles{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{"max_files": 80}})
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(result.Output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

func tuiSidebarDiff(cwd string) string {
	result, err := (tools.GitDiff{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{"max_lines": 120}})
	if err != nil {
		status, statusErr := (tools.GitStatus{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{}})
		if statusErr == nil {
			return status.Output
		}
		return ""
	}
	return result.Output
}

func tuiSessionPicker(store *session.Store, currentID string) (tui.PickerModal, error) {
	sessions, err := store.List()
	if err != nil {
		return tui.PickerModal{}, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	seen := map[string]bool{}
	options := make([]tui.PickerOption, 0, len(sessions)+1)
	for _, saved := range sessions {
		if strings.TrimSpace(saved.ID) == "" {
			continue
		}
		seen[saved.ID] = true
		options = append(options, tui.PickerOption{
			Value:       saved.ID,
			Label:       saved.ID,
			Description: fmt.Sprintf("%d messages · updated %s", len(saved.Messages), saved.UpdatedAt.Format("2006-01-02 15:04")),
		})
	}
	if strings.TrimSpace(currentID) != "" && !seen[currentID] {
		options = append([]tui.PickerOption{{
			Value:       currentID,
			Label:       currentID,
			Description: "current session · not saved yet",
		}}, options...)
	}
	if len(options) == 0 {
		return tui.PickerModal{}, fmt.Errorf("no saved sessions")
	}
	return tui.SessionPicker(currentID, options), nil
}

func tuiCheckpointRestorePicker(cwd string) (tui.PickerModal, error) {
	result, err := (tools.WorkspaceCheckpointList{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{}})
	if err != nil {
		return tui.PickerModal{}, err
	}
	var options []tui.PickerOption
	for _, line := range strings.Split(result.Output, "\n") {
		id := strings.TrimSpace(line)
		if id == "" || id == "no checkpoints" {
			continue
		}
		options = append(options, tui.PickerOption{
			Value:       id,
			Label:       id,
			Description: "restore workspace files from this checkpoint",
		})
	}
	if len(options) == 0 {
		return tui.PickerModal{}, fmt.Errorf("no checkpoints")
	}
	return tui.CheckpointRestorePicker(options), nil
}

func tuiLinesFromMessages(messages []llm.Message) []string {
	var lines []string
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		switch message.Role {
		case llm.RoleUser:
			lines = append(lines, "you: "+content)
		case llm.RoleAssistant:
			if strings.TrimSpace(message.Reasoning) != "" {
				lines = append(lines, "thoughts:0:"+message.Reasoning)
			}
			lines = append(lines, "agent: "+content)
			if message.DurationSeconds > 0 || (message.Usage != nil && message.Usage.TotalTokens > 0) {
				statsLine := fmt.Sprintf("stats: duration=%.1fs", message.DurationSeconds)
				if message.Usage != nil {
					statsLine += fmt.Sprintf(" input=%d cached=%d output=%d reasoning=%d total=%d",
						message.Usage.InputTokens,
						message.Usage.CachedTokens,
						message.Usage.OutputTokens,
						message.Usage.ReasoningTokens,
						message.Usage.TotalTokens,
					)
				}
				lines = append(lines, statsLine)
			}
		case llm.RoleTool:
			lines = append(lines, "tool:0:"+content)
		}
	}
	return lines
}

func formatTUISettings(cfg appconfig.Config, attachments []llm.Attachment) string {
	var builder strings.Builder
	builder.WriteString("Settings\n\n")
	fmt.Fprintf(&builder, "- provider: `%s`\n", valueOrString(cfg.Provider, "mock"))
	fmt.Fprintf(&builder, "- model: `%s`\n", valueOrString(cfg.Model, "provider env / routed"))
	fmt.Fprintf(&builder, "- endpoint: `%s`\n", valueOrString(providerBaseURL(cfg, cfg.Provider), "provider default/env"))
	for _, provider := range []string{"openai", "local", "ollama", "anthropic", "gemini"} {
		if endpoint := providerBaseURL(cfg, provider); endpoint != "" {
			fmt.Fprintf(&builder, "- %s endpoint: `%s`\n", provider, endpoint)
		}
	}
	fmt.Fprintf(&builder, "- reasoning: `%s`\n", valueOrString(cfg.Reasoning, "default"))
	fmt.Fprintf(&builder, "- stream: `%s`\n", onOff(cfg.Stream))
	fmt.Fprintf(&builder, "- sandbox: `%s`\n", valueOrString(cfg.Sandbox, "workspace-write"))
	fmt.Fprintf(&builder, "- mode: `%s`\n", valueOrString(cfg.Mode, "edit"))
	fmt.Fprintf(&builder, "- provider store: `%s`\n", onOff(cfg.StoreResponses))
	fmt.Fprintf(&builder, "- context cart: `%d files`\n", len(cfg.ContextFiles))
	fmt.Fprintf(&builder, "- approval: `%s`\n", valueOrString(cfg.ApprovalMode, "auto"))
	fmt.Fprintf(&builder, "- max context tokens: `%d`\n", cfg.MaxContext)
	fmt.Fprintf(&builder, "- max output tokens: `%d`\n", cfg.MaxOutput)
	fmt.Fprintf(&builder, "- max steps: `%d`\n", cfg.MaxSteps)
	fmt.Fprintf(&builder, "- max messages: `%d`\n", cfg.MaxMessages)
	fmt.Fprintf(&builder, "- max instruction bytes: `%d`\n", cfg.MaxInstructions)
	fmt.Fprintf(&builder, "- context cockpit: `%s`\n", onOff(cfg.ContextCockpit))
	fmt.Fprintf(&builder, "- cockpit inject: `%s`\n", onOff(cfg.ContextCockpitInject))
	fmt.Fprintf(&builder, "- cockpit budget: `%d tokens / %d files / %d cards`\n", cfg.ContextCockpitTokens, cfg.ContextCockpitMaxFiles, cfg.ContextCockpitMaxCards)
	fmt.Fprintf(&builder, "- cockpit diff: `%s`\n", onOff(cfg.ContextCockpitDiff))
	fmt.Fprintf(&builder, "- retrieval cart: `%s`\n", onOff(cfg.ContextRetrieval))
	fmt.Fprintf(&builder, "- retrieval budget: `%d tokens / %d chunks`\n", cfg.ContextRetrievalTokens, cfg.ContextRetrievalChunks)
	fmt.Fprintf(&builder, "- embeddings: `%s`\n", onOff(cfg.ContextEmbeddings))
	fmt.Fprintf(&builder, "- reranker: `%s`\n", onOff(cfg.ContextReranker))
	fmt.Fprintf(&builder, "- context recipes: `%s`\n", onOff(cfg.ContextRecipes))
	fmt.Fprintf(&builder, "- negative context: `%s`\n", onOff(cfg.NegativeContext))
	fmt.Fprintf(&builder, "- skills: `%s`\n", onOff(cfg.Skills))
	fmt.Fprintf(&builder, "- mcp servers: `%d`\n", len(configuredMCPServers(cfg)))
	fmt.Fprintf(&builder, "- fast model: `%s`\n", valueOrString(cfg.ModelRoutes["fast"], "default"))
	fmt.Fprintf(&builder, "- edit model: `%s`\n", valueOrString(cfg.ModelRoutes["edit"], "default"))
	fmt.Fprintf(&builder, "- deep model: `%s`\n", valueOrString(cfg.ModelRoutes["deep"], "default"))
	fmt.Fprintf(&builder, "- pending images: `%d`\n", len(attachments))
	builder.WriteString("\nUse `/provider`, `/model`, `/reasoning`, `/limits`, `/approval`, `/sandbox`, `/mode`, `/cart add`, `/context`, and `/attach` to change this without leaving Klyra.")
	return builder.String()
}

func formatSettingSaved(name, value string) string {
	return fmt.Sprintf("setting saved: %s = `%s`", name, valueOrString(value, "default"))
}

func applyTUISet(cfg *appconfig.Config, args []string) error {
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return fmt.Errorf("settings value must use key=value: %s", arg)
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "provider":
			cfg.Provider = value
			cfg.Model = ""
			if value == "mock" {
				cfg.Model = "mock-agent"
			}
		case "model":
			cfg.Model = value
		case "endpoint", "base_url", "base-url":
			setProviderBaseURL(cfg, cfg.Provider, value)
		case "endpoint_openai", "openai_endpoint":
			setProviderBaseURL(cfg, "openai", value)
		case "endpoint_local", "local_endpoint":
			setProviderBaseURL(cfg, "local", value)
		case "endpoint_ollama", "ollama_endpoint":
			setProviderBaseURL(cfg, "ollama", value)
		case "endpoint_anthropic", "anthropic_endpoint":
			setProviderBaseURL(cfg, "anthropic", value)
		case "endpoint_gemini", "gemini_endpoint":
			setProviderBaseURL(cfg, "gemini", value)
		case "reasoning":
			cfg.Reasoning = value
		case "stream":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("stream must be on/off")
			}
			cfg.Stream = parsed
		case "approval":
			cfg.ApprovalMode = value
		case "sandbox":
			cfg.Sandbox = value
		case "mode":
			cfg.Mode = value
		case "store", "store_responses", "store-responses":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("store must be on/off")
			}
			cfg.StoreResponses = parsed
		case "context":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context must be a positive integer")
			}
			cfg.MaxContext = parsed
		case "output":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("output must be a positive integer")
			}
			cfg.MaxOutput = parsed
		case "steps":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("steps must be a positive integer")
			}
			cfg.MaxSteps = parsed
		case "messages":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("messages must be a positive integer")
			}
			cfg.MaxMessages = parsed
		case "instructions", "instruction_bytes", "instruction-bytes":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("instructions must be a positive integer")
			}
			cfg.MaxInstructions = parsed
		case "context_cockpit", "cockpit":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_cockpit must be on/off")
			}
			cfg.ContextCockpit = parsed
		case "context_cockpit_inject", "cockpit_inject":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_cockpit_inject must be on/off")
			}
			cfg.ContextCockpitInject = parsed
		case "context_cockpit_diff", "cockpit_diff":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_cockpit_diff must be on/off")
			}
			cfg.ContextCockpitDiff = parsed
		case "context_cockpit_tokens", "cockpit_tokens":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context_cockpit_tokens must be a positive integer")
			}
			cfg.ContextCockpitTokens = parsed
		case "context_cockpit_files", "cockpit_files":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context_cockpit_files must be a positive integer")
			}
			cfg.ContextCockpitMaxFiles = parsed
		case "context_cockpit_cards", "cockpit_cards":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context_cockpit_cards must be a positive integer")
			}
			cfg.ContextCockpitMaxCards = parsed
		case "context_retrieval", "retrieval":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_retrieval must be on/off")
			}
			cfg.ContextRetrieval = parsed
		case "context_retrieval_tokens", "retrieval_tokens":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context_retrieval_tokens must be a positive integer")
			}
			cfg.ContextRetrievalTokens = parsed
		case "context_retrieval_chunks", "retrieval_chunks":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context_retrieval_chunks must be a positive integer")
			}
			cfg.ContextRetrievalChunks = parsed
		case "context_embeddings", "embeddings":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_embeddings must be on/off")
			}
			cfg.ContextEmbeddings = parsed
		case "context_reranker", "reranker":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_reranker must be on/off")
			}
			cfg.ContextReranker = parsed
		case "context_recipes", "recipes":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_recipes must be on/off")
			}
			cfg.ContextRecipes = parsed
		case "negative_context", "negative":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("negative_context must be on/off")
			}
			cfg.NegativeContext = parsed
		case "skills":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("skills must be on/off")
			}
			cfg.Skills = parsed
		case "fast_model", "fast-model":
			setModelRoute(cfg, "fast", value)
		case "edit_model", "edit-model":
			setModelRoute(cfg, "edit", value)
		case "deep_model", "deep-model":
			setModelRoute(cfg, "deep", value)
		case "disabled_tools", "disabled-tools":
			if value == "" {
				cfg.DisabledTools = nil
			} else {
				parts := strings.Split(value, ",")
				cleaned := []string{}
				for _, p := range parts {
					if t := strings.TrimSpace(p); t != "" {
						cleaned = append(cleaned, t)
					}
				}
				cfg.DisabledTools = cleaned
			}
		default:
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	return nil
}

func setModelRoute(cfg *appconfig.Config, route, model string) {
	if cfg.ModelRoutes == nil {
		cfg.ModelRoutes = map[string]string{}
	}
	model = strings.TrimSpace(model)
	if model == "" {
		delete(cfg.ModelRoutes, route)
		return
	}
	cfg.ModelRoutes[route] = model
}

func formatContextCart(files []string) string {
	if len(files) == 0 {
		return "Context Cart\n\n*Cart is empty. Use `/cart add <file>` to attach files.*"
	}
	var builder strings.Builder
	builder.WriteString("Context Cart\n\n")
	for _, file := range files {
		fmt.Fprintf(&builder, "- `%s`\n", file)
	}
	return builder.String()
}

func formatContextCockpit(cfg appconfig.Config, cwd, focus string) (string, error) {
	snapshot, err := cockpit.Build(context.Background(), cockpit.Config{
		Enabled:          cfg.ContextCockpit,
		Inject:           cfg.ContextCockpitInject,
		MaxTokens:        cfg.ContextCockpitTokens,
		MaxFiles:         cfg.ContextCockpitMaxFiles,
		MaxCards:         cfg.ContextCockpitMaxCards,
		IncludeDiff:      cfg.ContextCockpitDiff,
		IncludeRetrieval: cfg.ContextRetrieval,
		RetrievalTokens:  cfg.ContextRetrievalTokens,
		RetrievalChunks:  cfg.ContextRetrievalChunks,
		UseEmbeddings:    cfg.ContextEmbeddings,
		UseReranker:      cfg.ContextReranker,
		IncludeRecipes:   cfg.ContextRecipes,
		IncludeNegative:  cfg.NegativeContext,
		MaxInstructions:  cfg.MaxInstructions,
	}, cwd, focus, cfg.ContextFiles)
	if err != nil {
		return "", err
	}
	return "Context Cockpit\n\n" + snapshot.Markdown(), nil
}

func printContextDebug(out io.Writer, debug agent.ContextDebug) {
	text := formatContextDebug(debug)
	if strings.TrimSpace(text) != "" {
		fmt.Fprintln(out, text)
	}
}

func formatContextDebug(debug agent.ContextDebug) string {
	if debug.Mode == "" && len(debug.VisibleTools) == 0 && len(debug.Risks) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("Context Debugger\n\n")
	fmt.Fprintf(&builder, "- mode: `%s`\n", valueOrString(debug.Mode, "edit"))
	if len(debug.ContextFiles) == 0 {
		builder.WriteString("- context cart: empty\n")
	} else {
		builder.WriteString("- context cart:\n")
		for _, file := range debug.ContextFiles {
			builder.WriteString("  - `" + file + "`\n")
		}
	}
	if len(debug.VisibleTools) > 0 {
		builder.WriteString("- model could use: `" + strings.Join(debug.VisibleTools, "`, `") + "`\n")
	}
	if len(debug.Risks) > 0 {
		builder.WriteString("- risks:\n")
		for _, risk := range debug.Risks {
			builder.WriteString("  - " + risk + "\n")
		}
	}
	if strings.TrimSpace(debug.Cockpit) != "" {
		fmt.Fprintf(&builder, "\nContext Cockpit\n\n- estimated tokens: `%d`\n\n%s\n", debug.CockpitTokens, debug.Cockpit)
	}
	return strings.TrimSpace(builder.String())
}

func applyTUILimit(cfg *appconfig.Config, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: /limits context|output|steps|messages|instructions <number>")
	}
	value, err := strconv.Atoi(args[1])
	if err != nil || value <= 0 {
		return fmt.Errorf("limit must be a positive integer")
	}
	switch strings.ToLower(args[0]) {
	case "context", "ctx":
		cfg.MaxContext = value
	case "output", "out":
		cfg.MaxOutput = value
	case "steps":
		cfg.MaxSteps = value
	case "messages":
		cfg.MaxMessages = value
	case "instructions", "instruction-bytes":
		cfg.MaxInstructions = value
	default:
		return fmt.Errorf("unknown limit %q", args[0])
	}
	return nil
}

func loadImageAttachment(cwd, path string) (llm.Attachment, error) {
	path = strings.Trim(path, "\"'")
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return llm.Attachment{}, err
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(target)))
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = mimeType[:idx]
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return llm.Attachment{}, fmt.Errorf("%s is not a supported image file", path)
	}
	return llm.Attachment{
		Type:     "image",
		MIMEType: mimeType,
		Name:     filepath.Base(target),
		Data:     base64.StdEncoding.EncodeToString(data),
	}, nil
}

func formatAttachments(attachments []llm.Attachment) string {
	if len(attachments) == 0 {
		return "Pending Attachments\n\n*No pending image attachments.*"
	}
	var builder strings.Builder
	builder.WriteString("Pending Attachments\n\n")
	for i, attachment := range attachments {
		fmt.Fprintf(&builder, "%d. `%s` (%s, `%d` bytes)\n", i+1, attachment.Name, attachment.MIMEType, len(attachment.Data))
	}
	return builder.String()
}

func valueOrString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func parseBoolSetting(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true, nil
	case "0", "false", "no", "off", "disable", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, "\n\n")
}

func newDiffCommand(opts *options) *cobra.Command {
	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Preview and validate unified diffs",
	}
	diffCmd.AddCommand(&cobra.Command{
		Use:   "preview [patch-file]",
		Short: "Validate a unified diff without applying it; reads stdin when no file is provided",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch, err := readPatchInput(args)
			if err != nil {
				return err
			}
			result, err := (tools.DiffPreview{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"patch": patch},
			})
			if result.Output != "" {
				fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			}
			return err
		},
	})
	var yes bool
	var checkpoint bool
	applyCmd := &cobra.Command{
		Use:   "apply [patch-file]",
		Short: "Preview, checkpoint, and apply a unified diff",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch, err := readPatchInput(args)
			if err != nil {
				return err
			}
			preview, err := (tools.DiffPreview{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"patch": patch},
			})
			if preview.Output != "" {
				fmt.Fprintln(cmd.OutOrStdout(), preview.Output)
			}
			if err != nil {
				return err
			}
			if !yes && !confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "apply patch? [y/N]: ") {
				return fmt.Errorf("patch apply cancelled")
			}
			if checkpoint {
				id := "before-patch-" + time.Now().UTC().Format("20060102-150405")
				result, err := (tools.WorkspaceCheckpoint{}).Run(context.Background(), tools.Invocation{
					CWD:  opts.cwd,
					Args: map[string]any{"id": id},
				})
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			}
			result, err := (tools.DiffPatcher{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"patch": patch},
			})
			if result.Output != "" {
				fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			}
			return err
		},
	}
	applyCmd.Flags().BoolVar(&yes, "yes", false, "apply without interactive confirmation")
	applyCmd.Flags().BoolVar(&checkpoint, "checkpoint", true, "create workspace checkpoint before applying")
	diffCmd.AddCommand(applyCmd)
	return diffCmd
}

func newPolicyCommand() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect local safety policy decisions",
	}
	var sandbox string
	checkCmd := &cobra.Command{
		Use:   "check [command]",
		Short: "Classify a shell command by risk",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			assessment := policy.AssessShellCommand(strings.Join(args, " "))
			allowed, reason := policy.IsAllowedInSandbox(assessment, policy.NormalizeSandbox(sandbox))
			payload := map[string]any{
				"assessment": assessment,
				"sandbox":    policy.NormalizeSandbox(sandbox),
				"allowed":    allowed,
				"reason":     reason,
			}
			data, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	checkCmd.Flags().StringVar(&sandbox, "sandbox", "workspace-write", "sandbox profile to evaluate")
	policyCmd.AddCommand(checkCmd)
	return policyCmd
}

func confirm(input io.Reader, output io.Writer, prompt string) bool {
	fmt.Fprint(output, prompt)
	reader := bufio.NewReader(input)
	answer, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(answer) == "" {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func readPatchInput(args []string) (string, error) {
	if len(args) > 0 {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func newStatusCommand(opts *options) *cobra.Command {
	var showDiff bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show compact workspace status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := (tools.GitStatus{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{}})
			if err != nil {
				return err
			}
			if strings.TrimSpace(status.Output) == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "clean")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), status.Output)
			}
			if showDiff {
				diff, err := (tools.GitDiff{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{"max_lines": 240}})
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "\ndiff:")
				fmt.Fprintln(cmd.OutOrStdout(), diff.Output)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showDiff, "diff", false, "include compressed tracked diff")
	return cmd
}

func newCheckpointCommand(opts *options) *cobra.Command {
	checkpointCmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Create, list, and restore workspace checkpoints",
	}
	checkpointCmd.AddCommand(&cobra.Command{
		Use:   "create [id]",
		Short: "Create a workspace checkpoint",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) > 0 {
				id = args[0]
			}
			result, err := (tools.WorkspaceCheckpoint{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"id": id},
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			return nil
		},
	})
	checkpointCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List workspace checkpoints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := (tools.WorkspaceCheckpointList{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{}})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			return nil
		},
	})
	checkpointCmd.AddCommand(&cobra.Command{
		Use:   "restore [id]",
		Short: "Restore files from a workspace checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := (tools.WorkspaceRestore{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"id": args[0]},
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			return nil
		},
	})
	return checkpointCmd
}

func newChatCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive coding session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			provider, model, err := buildProviderFromConfig(runtimeCfg)
			if err != nil {
				return err
			}
			toolRegistry, err := buildToolRegistry(context.Background(), runtimeCfg)
			if err != nil {
				return err
			}
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.LoadOrCreate(opts.sessionID, opts.cwd)
			if err != nil {
				return err
			}
			baseCfg := buildBaseAgentConfig(runtimeCfg, opts.cwd, model, provider, toolRegistry)
			baseCfg.Input = os.Stdin
			baseCfg.Output = cmd.OutOrStdout()
			// Enable streaming by default in interactive chat unless the user passed --no-stream
			if !cmd.Root().PersistentFlags().Changed("no-stream") {
				baseCfg.Stream = true
			}
			runner, err := agent.New(baseCfg)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			fmt.Fprintf(cmd.OutOrStdout(), "session: %s\n", saved.ID)
			fmt.Fprintln(cmd.OutOrStdout(), "type /exit to quit, /save to persist now, /compact to compress history")
			scanner := bufio.NewScanner(os.Stdin)
			for {
				fmt.Fprint(cmd.OutOrStdout(), "> ")
				if !scanner.Scan() {
					break
				}
				line := strings.TrimSpace(scanner.Text())
				switch line {
				case "":
					continue
				case "/exit", "/quit":
					return store.Save(saved)
				case "/help":
					fmt.Fprintln(cmd.OutOrStdout(), "commands: /help, /status, /compact, /save, /exit")
					continue
				case "/status":
					status, err := tuiStatus(opts.cwd)
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), status)
					continue
				case "/save":
					if err := store.Save(saved); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "saved: %s\n", saved.ID)
					continue
				case "/compact":
					compacted, stats := contextmgr.CompactMessages(saved.Messages, runtimeCfg.MaxContext, runtimeCfg.MaxMessages/2)
					saved.Messages = compacted
					if err := store.Save(saved); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "compacted: messages %d -> %d, estimated tokens %d -> %d\n",
						stats.OriginalMessages, stats.PackedMessages, stats.OriginalTokens, stats.PackedTokens)
					continue
				}
				result, runErr := runner.RunConversation(ctx, saved.Messages, line)
				saved.Messages = result.Messages
				if saveErr := store.Save(saved); saveErr != nil {
					return saveErr
				}
				printContextDebug(cmd.OutOrStdout(), result.ContextDebug)
				if runErr != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "error: %v\n", runErr)
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			return store.Save(saved)
		},
	}
}

func newToolsCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List tools available to the agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			registry, err := buildToolRegistry(context.Background(), runtimeCfg)
			if err != nil {
				return err
			}
			specs := registry.Specs()
			sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
			for _, spec := range specs {
				status := "enabled"
				if registry.IsDisabled(spec.Name) {
					status = "disabled"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-24s [%s] %s\n", spec.Name, status, spec.Description)
			}
			return nil
		},
	}
}

func newInstructionsCommand(opts *options) *cobra.Command {
	var showContent bool
	cmd := &cobra.Command{
		Use:   "instructions",
		Short: "Show project instruction files loaded into the system prompt",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			result, err := instructions.Load(opts.cwd, runtimeCfg.MaxInstructions)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(result.Files) == 0 {
				fmt.Fprintln(out, "no project instructions found")
				return nil
			}
			for _, file := range result.Files {
				suffix := ""
				if file.Truncated {
					suffix = " truncated"
				}
				fmt.Fprintf(out, "%s bytes=%d%s\n", file.Path, file.Bytes, suffix)
			}
			if result.Truncated {
				fmt.Fprintf(out, "instruction budget reached: %d bytes\n", runtimeCfg.MaxInstructions)
			}
			if showContent {
				fmt.Fprintln(out, "\ncontent:")
				fmt.Fprintln(out, result.Content)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showContent, "content", false, "print loaded instruction content")
	return cmd
}

func newSkillsCommand(opts *options) *cobra.Command {
	var showContent bool
	var all bool
	var query string
	cmd := &cobra.Command{
		Use:   "skills [query]",
		Short: "Show project skills matched into the system prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			var result skills.Result
			focus := strings.TrimSpace(query)
			if focus == "" {
				focus = strings.Join(args, " ")
			}
			if all || (strings.TrimSpace(focus) == "" && len(runtimeCfg.ContextFiles) == 0) {
				result, err = skills.LoadAll(opts.cwd, runtimeCfg.MaxInstructions/2)
			} else {
				result, err = skills.Load(opts.cwd, focus, runtimeCfg.ContextFiles, runtimeCfg.MaxInstructions/2)
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(result.Skills) == 0 {
				fmt.Fprintln(out, "no project skills found")
				return nil
			}
			for _, skill := range result.Skills {
				suffix := ""
				if skill.Truncated {
					suffix = " truncated"
				}
				reason := ""
				if skill.Reason != "" {
					reason = " reason=" + skill.Reason
				}
				fmt.Fprintf(out, "%s name=%q bytes=%d%s%s\n", skill.Path, skill.Name, skill.Bytes, suffix, reason)
			}
			if result.Truncated {
				fmt.Fprintf(out, "skill budget reached: %d bytes\n", runtimeCfg.MaxInstructions/2)
			}
			if showContent {
				fmt.Fprintln(out, "\ncontent:")
				fmt.Fprintln(out, result.Content)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showContent, "content", false, "print loaded skill content")
	cmd.Flags().BoolVar(&all, "all", false, "show all discovered skills instead of matched skills")
	cmd.Flags().StringVar(&query, "query", "", "task text used to match skills")
	return cmd
}

func newDoctorCommand(opts *options) *cobra.Command {
	var pingAPI bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local runtime support for klyra",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "klyra: %s\n", version.Version)
			fmt.Fprintf(out, "go: %s\n", runtime.Version())
			fmt.Fprintf(out, "os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			printExecutableStatus(out, "git")
			printExecutableStatus(out, "rg")
			printEnvStatus(out, "OPENAI_API_KEY")
			printEnvStatus(out, "OPENAI_MODEL")
			printEnvStatus(out, "OPENAI_BASE_URL")
			printEnvStatus(out, "OLLAMA_MODEL")
			printEnvStatus(out, "OLLAMA_BASE_URL")
			printEnvStatus(out, "ANTHROPIC_API_KEY")
			printEnvStatus(out, "ANTHROPIC_MODEL")
			printEnvStatus(out, "ANTHROPIC_BASE_URL")
			printEnvStatus(out, "GEMINI_API_KEY")
			printEnvStatus(out, "GEMINI_MODEL")
			printEnvStatus(out, "GEMINI_BASE_URL")
			projectInstructions, err := instructions.Load(opts.cwd, runtimeCfg.MaxInstructions)
			if err != nil {
				return err
			}
			if len(projectInstructions.Files) == 0 {
				fmt.Fprintln(out, "project_instructions: none")
			} else {
				names := make([]string, 0, len(projectInstructions.Files))
				for _, file := range projectInstructions.Files {
					names = append(names, file.Path)
				}
				fmt.Fprintf(out, "project_instructions: %s (%d bytes)\n", strings.Join(names, ", "), projectInstructions.Bytes)
			}
			projectSkills, err := skills.LoadAll(opts.cwd, runtimeCfg.MaxInstructions/2)
			if err != nil {
				return err
			}
			if len(projectSkills.Skills) == 0 {
				fmt.Fprintln(out, "project_skills: none")
			} else {
				names := make([]string, 0, len(projectSkills.Skills))
				for _, skill := range projectSkills.Skills {
					names = append(names, skill.Path)
				}
				fmt.Fprintf(out, "project_skills: %s (%d bytes)\n", strings.Join(names, ", "), projectSkills.Bytes)
			}
			if pingAPI {
				provider, model, buildErr := buildProviderFromConfig(runtimeCfg)
				if buildErr != nil {
					fmt.Fprintf(out, "ping: provider build failed: %v\n", buildErr)
				} else {
					start := time.Now()
					_, pingErr := provider.Complete(context.Background(), llm.Request{
						Model:           model,
						Messages:        []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
						MaxOutputTokens: 1,
					})
					elapsed := time.Since(start)
					if pingErr != nil {
						fmt.Fprintf(out, "ping: FAIL (%v)\n", pingErr)
					} else {
						fmt.Fprintf(out, "ping: OK (%dms)\n", elapsed.Milliseconds())
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&pingAPI, "ping", false, "test API connectivity with a minimal request")
	return cmd
}

func newConfigCommand(opts *options) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage klyra configuration",
	}
	configCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Write a default config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := appconfig.WriteDefault(opts.configPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	})
	configCmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print effective configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(runtimeCfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	})
	configCmd.AddCommand(&cobra.Command{
		Use:   "profiles",
		Short: "List available config profiles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := appconfig.Load(opts.configPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(cfg.Profiles) == 0 {
				fmt.Fprintln(out, "no profiles defined")
				return nil
			}
			names := make([]string, 0, len(cfg.Profiles))
			for name := range cfg.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				p := cfg.Profiles[name]
				var parts []string
				if p.Provider != "" {
					parts = append(parts, "provider="+p.Provider)
				}
				if p.Model != "" {
					parts = append(parts, "model="+p.Model)
				}
				if p.Reasoning != "" {
					parts = append(parts, "reasoning="+p.Reasoning)
				}
				if p.MaxSteps > 0 {
					parts = append(parts, fmt.Sprintf("max_steps=%d", p.MaxSteps))
				}
				if p.ApprovalMode != "" {
					parts = append(parts, "approval="+p.ApprovalMode)
				}
				if len(parts) > 0 {
					fmt.Fprintf(out, "%-16s  %s\n", name, strings.Join(parts, " "))
				} else {
					fmt.Fprintln(out, name)
				}
			}
			return nil
		},
	})
	return configCmd
}

func newSessionsCommand(opts *options) *cobra.Command {
	sessionsCmd := &cobra.Command{
		Use:   "sessions",
		Short: "List saved workspace sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			sessions, err := store.List()
			if err != nil {
				return err
			}
			sort.Slice(sessions, func(i, j int) bool {
				return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
			})
			for _, session := range sessions {
				fmt.Fprintf(cmd.OutOrStdout(), "%s messages=%d updated=%s\n", session.ID, len(session.Messages), session.UpdatedAt.Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
	sessionsCmd.AddCommand(&cobra.Command{
		Use:   "compact [id]",
		Short: "Compact a saved session to reduce future context tokens",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.Load(args[0])
			if err != nil {
				return err
			}
			compacted, stats := contextmgr.CompactMessages(saved.Messages, runtimeCfg.MaxContext, runtimeCfg.MaxMessages/2)
			saved.Messages = compacted
			if err := store.Save(saved); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "compacted %s: messages %d -> %d, estimated tokens %d -> %d\n",
				saved.ID, stats.OriginalMessages, stats.PackedMessages, stats.OriginalTokens, stats.PackedTokens)
			return nil
		},
	})
	sessionsCmd.AddCommand(&cobra.Command{
		Use:   "rename [old-id] [new-id]",
		Short: "Rename a saved session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			if err := store.Rename(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed session: %s -> %s\n", args[0], args[1])
			return nil
		},
	})
	sessionsCmd.AddCommand(&cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a saved session by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			if err := store.Delete(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted session: %s\n", args[0])
			return nil
		},
	})
	var pruneDays int
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete sessions not updated within --days days (default 30)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			count, err := store.Prune(time.Duration(pruneDays) * 24 * time.Hour)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pruned %d session(s) older than %d days\n", count, pruneDays)
			return nil
		},
	}
	pruneCmd.Flags().IntVar(&pruneDays, "days", 30, "delete sessions not updated within this many days")
	sessionsCmd.AddCommand(pruneCmd)

	exportCmd := &cobra.Command{
		Use:   "export [id] [file]",
		Short: "Export a session to a JSON file (defaults to <id>.json)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			id := args[0]
			dest := id + ".json"
			if len(args) == 2 {
				dest = args[1]
			}
			if _, err := store.Export(id, dest); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported session %s -> %s\n", id, dest)
			return nil
		},
	}
	sessionsCmd.AddCommand(exportCmd)

	var importOverwrite bool
	importCmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Import a session from a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			sess, err := store.Import(args[0], importOverwrite)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported session: %s (%d messages)\n", sess.ID, len(sess.Messages))
			return nil
		},
	}
	importCmd.Flags().BoolVar(&importOverwrite, "overwrite", false, "overwrite existing session with same id")
	sessionsCmd.AddCommand(importCmd)

	return sessionsCmd
}

func newInitCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a klyra project in the current directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			cwd := opts.cwd
			klyraDir := filepath.Join(cwd, ".klyra")
			if err := os.MkdirAll(klyraDir, 0o755); err != nil {
				return err
			}
			created := 0

			instrPath := filepath.Join(klyraDir, "instructions.md")
			if _, err := os.Stat(instrPath); os.IsNotExist(err) {
				instrContent := "# Project Instructions\n\nDescribe your project and coding conventions here.\nklyra will include this in every agent run.\n\n## Guidelines\n\n- Prefer small, focused changes\n- Write tests for new functionality\n- Document public APIs\n"
				if err := os.WriteFile(instrPath, []byte(instrContent), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "created .klyra/instructions.md\n")
				created++
			} else {
				fmt.Fprintf(out, "skipped .klyra/instructions.md (already exists)\n")
			}

			ignorePath := filepath.Join(klyraDir, "ignore.md")
			if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
				ignoreContent := "# klyra ignore patterns\n# Files and directories listed here are excluded from list_files, search, and project_map.\n# Syntax: one glob pattern per line. Lines starting with # are comments.\n\ndist/\nbuild/\nout/\n*.min.js\n*.min.css\n*.generated.go\n*.pb.go\n"
				if err := os.WriteFile(ignorePath, []byte(ignoreContent), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "created .klyra/ignore.md\n")
				created++
			} else {
				fmt.Fprintf(out, "skipped .klyra/ignore.md (already exists)\n")
			}

			agentDir := filepath.Join(cwd, ".agentcli")
			if err := os.MkdirAll(agentDir, 0o755); err != nil {
				return err
			}

			cfgPath := filepath.Join(agentDir, "config.json")
			if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
				if _, err := appconfig.WriteDefault(cfgPath); err != nil {
					return err
				}
				fmt.Fprintf(out, "created .agentcli/config.json\n")
				created++
			} else {
				fmt.Fprintf(out, "skipped .agentcli/config.json (already exists)\n")
			}

			if created > 0 {
				fmt.Fprintf(out, "\nProject initialized. Edit .klyra/instructions.md to describe your project.\nSet OPENAI_API_KEY / ANTHROPIC_API_KEY / GEMINI_API_KEY and run: klyra run \"hello\"\n")
			} else {
				fmt.Fprintf(out, "\nAlready initialized.\n")
			}
			return nil
		},
	}
}

func effectiveConfig(cmd *cobra.Command, opts options) (appconfig.Config, error) {
	loadEnvFile(opts.cwd)
	cfg, err := appconfig.Load(opts.configPath)
	if err != nil {
		return appconfig.Config{}, err
	}
	cfg, err = cfg.WithProfile(opts.profile)
	if err != nil {
		return appconfig.Config{}, err
	}
	flags := cmd.Root().PersistentFlags()
	if flags.Changed("provider") {
		cfg.Provider = opts.provider
	}
	if flags.Changed("model") {
		cfg.Model = opts.model
	}
	if flags.Changed("base-url") {
		setProviderBaseURL(&cfg, cfg.Provider, opts.baseURL)
	}
	if cfg.ModelRoutes == nil {
		cfg.ModelRoutes = map[string]string{}
	}
	if flags.Changed("fast-model") {
		cfg.ModelRoutes["fast"] = opts.fastModel
	}
	if flags.Changed("edit-model") {
		cfg.ModelRoutes["edit"] = opts.editModel
	}
	if flags.Changed("deep-model") {
		cfg.ModelRoutes["deep"] = opts.deepModel
	}
	if flags.Changed("max-steps") {
		cfg.MaxSteps = opts.maxSteps
	}
	if flags.Changed("max-messages") {
		cfg.MaxMessages = opts.maxMessages
	}
	if flags.Changed("max-context-tokens") {
		cfg.MaxContext = opts.maxContext
	}
	if flags.Changed("max-instruction-bytes") {
		cfg.MaxInstructions = opts.maxInstructions
	}
	if flags.Changed("max-output-tokens") {
		cfg.MaxOutput = opts.maxOutput
	}
	if flags.Changed("reasoning") {
		cfg.Reasoning = opts.reasoning
	}
	if flags.Changed("stream") {
		cfg.Stream = opts.stream
	}
	if flags.Changed("no-stream") {
		cfg.Stream = !opts.noStream
	}
	if flags.Changed("approval") {
		cfg.ApprovalMode = opts.approval
	}
	if flags.Changed("sandbox") {
		cfg.Sandbox = opts.sandbox
	}
	if flags.Changed("mode") {
		cfg.Mode = opts.mode
	}
	if flags.Changed("context-file") {
		cfg.ContextFiles = append([]string(nil), opts.contextFiles...)
	}
	if flags.Changed("context-cockpit") {
		cfg.ContextCockpit = opts.contextCockpit
	}
	if flags.Changed("no-context-cockpit") {
		cfg.ContextCockpit = !opts.noContextCockpit
	}
	if flags.Changed("context-cockpit-inject") {
		cfg.ContextCockpitInject = opts.contextCockpitInject
	}
	if flags.Changed("no-context-cockpit-inject") {
		cfg.ContextCockpitInject = !opts.noContextCockpitInject
	}
	if flags.Changed("context-cockpit-tokens") {
		cfg.ContextCockpitTokens = opts.contextCockpitTokens
	}
	if flags.Changed("context-cockpit-files") {
		cfg.ContextCockpitMaxFiles = opts.contextCockpitMaxFiles
	}
	if flags.Changed("context-cockpit-cards") {
		cfg.ContextCockpitMaxCards = opts.contextCockpitMaxCards
	}
	if flags.Changed("context-retrieval") {
		cfg.ContextRetrieval = opts.contextRetrieval
	}
	if flags.Changed("no-context-retrieval") {
		cfg.ContextRetrieval = !opts.noContextRetrieval
	}
	if flags.Changed("context-retrieval-tokens") {
		cfg.ContextRetrievalTokens = opts.contextRetrievalTokens
	}
	if flags.Changed("context-retrieval-chunks") {
		cfg.ContextRetrievalChunks = opts.contextRetrievalChunks
	}
	if flags.Changed("context-embeddings") {
		cfg.ContextEmbeddings = opts.contextEmbeddings
	}
	if flags.Changed("no-context-embeddings") {
		cfg.ContextEmbeddings = !opts.noContextEmbeddings
	}
	if flags.Changed("context-reranker") {
		cfg.ContextReranker = opts.contextReranker
	}
	if flags.Changed("no-context-reranker") {
		cfg.ContextReranker = !opts.noContextReranker
	}
	if flags.Changed("context-recipes") {
		cfg.ContextRecipes = opts.contextRecipes
	}
	if flags.Changed("no-context-recipes") {
		cfg.ContextRecipes = !opts.noContextRecipes
	}
	if flags.Changed("skills") {
		cfg.Skills = opts.skills
	}
	if flags.Changed("no-skills") {
		cfg.Skills = !opts.noSkills
	}
	if flags.Changed("negative-context") {
		cfg.NegativeContext = opts.negativeContext
	}
	if flags.Changed("no-negative-context") {
		cfg.NegativeContext = !opts.noNegativeContext
	}
	if flags.Changed("store") {
		cfg.StoreResponses = opts.store
	}
	return cfg, nil
}

func printExecutableStatus(out interface{ Write([]byte) (int, error) }, name string) {
	path, err := exec.LookPath(name)
	if err != nil {
		fmt.Fprintf(out, "%s: missing\n", name)
		return
	}
	fmt.Fprintf(out, "%s: %s\n", name, path)
}

func printEnvStatus(out interface{ Write([]byte) (int, error) }, name string) {
	if strings.TrimSpace(os.Getenv(name)) == "" {
		fmt.Fprintf(out, "%s: unset\n", name)
		return
	}
	fmt.Fprintf(out, "%s: set\n", name)
}

// buildBaseAgentConfig constructs the common agent.Config from a runtime config.
// Callers must still set Output, Input, and any callback fields (Approver, StreamHandler, etc.).
func buildBaseAgentConfig(runtimeCfg appconfig.Config, cwd, model string, provider llm.Provider, toolRegistry *tools.Registry) agent.Config {
	baseCfg := agent.Config{
		CWD:                    cwd,
		Model:                  model,
		ModelRoutes:            runtimeCfg.ModelRoutes,
		MaxSteps:               runtimeCfg.MaxSteps,
		MaxMessages:            runtimeCfg.MaxMessages,
		MaxContext:             runtimeCfg.MaxContext,
		MaxInstructions:        runtimeCfg.MaxInstructions,
		MaxOutput:              runtimeCfg.MaxOutput,
		Reasoning:              runtimeCfg.Reasoning,
		Store:                  runtimeCfg.StoreResponses,
		Stream:                 runtimeCfg.Stream,
		ApprovalMode:           runtimeCfg.ApprovalMode,
		Sandbox:                runtimeCfg.Sandbox,
		Mode:                   runtimeCfg.Mode,
		ContextFiles:           runtimeCfg.ContextFiles,
		ContextCockpitEnabled:  runtimeCfg.ContextCockpit,
		ContextCockpitInject:   runtimeCfg.ContextCockpitInject,
		ContextCockpitTokens:   runtimeCfg.ContextCockpitTokens,
		ContextCockpitMaxFiles: runtimeCfg.ContextCockpitMaxFiles,
		ContextCockpitMaxCards: runtimeCfg.ContextCockpitMaxCards,
		ContextRetrieval:       runtimeCfg.ContextRetrieval,
		ContextRetrievalTokens: runtimeCfg.ContextRetrievalTokens,
		ContextRetrievalChunks: runtimeCfg.ContextRetrievalChunks,
		ContextEmbeddings:      runtimeCfg.ContextEmbeddings,
		ContextReranker:        runtimeCfg.ContextReranker,
		ContextCockpitDiff:     runtimeCfg.ContextCockpitDiff,
		ContextRecipes:         runtimeCfg.ContextRecipes,
		NegativeContext:        runtimeCfg.NegativeContext,
		Skills:                 runtimeCfg.Skills,
		Provider:               provider,
		Tools:                  toolRegistry,
	}
	baseCfg.SubAgentFactory = agent.DefaultSubAgentFactory(baseCfg)
	return baseCfg
}

func buildProvider(name, model string) (llm.Provider, string, error) {
	return buildProviderWithBaseURL(name, model, "")
}

func buildProviderFromConfig(cfg appconfig.Config) (llm.Provider, string, error) {
	return buildProviderWithBaseURL(cfg.Provider, cfg.Model, providerBaseURL(cfg, cfg.Provider))
}

func buildProviderWithBaseURL(name, model, baseURL string) (llm.Provider, string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "", "mock":
		if strings.TrimSpace(model) == "" {
			model = "mock-agent"
		}
		return llm.NewMockProvider(), model, nil
	case "openai", "responses":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("OPENAI_BASE_URL")
		}
		provider, err := llm.NewResponsesProvider(os.Getenv("OPENAI_API_KEY"), baseURL)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OPENAI_MODEL")
		}
		return provider, model, nil
	case "local", "chat", "chat-completions", "openai-chat", "openai-compatible":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("LOCAL_BASE_URL")
		}
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("OPENAI_BASE_URL")
		}
		if strings.TrimSpace(baseURL) == "" && normalized == "local" {
			// Sensible default for local testing if not using Ollama
			baseURL = "http://localhost:8080/v1"
		}
		provider, err := llm.NewOpenAIProvider(os.Getenv("OPENAI_API_KEY"), baseURL)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OPENAI_MODEL")
			if model == "" {
				model = "local-model" // fallback so it doesn't fail
			}
		}
		return provider, model, nil
	case "ollama":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("OLLAMA_BASE_URL")
		}
		if strings.TrimSpace(baseURL) == "" {
			baseURL = "http://localhost:11434/v1"
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OLLAMA_MODEL")
		}
		provider, err := llm.NewOpenAIProvider("ollama", baseURL)
		if err != nil {
			return nil, "", err
		}
		return provider, model, nil
	case "anthropic", "claude":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("ANTHROPIC_BASE_URL")
		}
		provider, err := llm.NewAnthropicProvider(os.Getenv("ANTHROPIC_API_KEY"), baseURL)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("ANTHROPIC_MODEL")
		}
		if strings.TrimSpace(model) == "" {
			return nil, "", fmt.Errorf("model is required for provider %q; pass --model or set ANTHROPIC_MODEL", normalized)
		}
		return provider, model, nil
	case "gemini", "google":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("GEMINI_BASE_URL")
		}
		provider, err := llm.NewGeminiProvider(os.Getenv("GEMINI_API_KEY"), baseURL)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("GEMINI_MODEL")
		}
		if strings.TrimSpace(model) == "" {
			return nil, "", fmt.Errorf("model is required for provider %q; pass --model or set GEMINI_MODEL", normalized)
		}
		return provider, model, nil
	default:
		return nil, "", fmt.Errorf("provider %q is not implemented yet", name)
	}
}

func providerBaseURL(cfg appconfig.Config, provider string) string {
	if cfg.BaseURLs == nil {
		return ""
	}
	return cfg.BaseURLs[strings.ToLower(strings.TrimSpace(provider))]
}

func buildToolRegistry(ctx context.Context, cfg appconfig.Config) (*tools.Registry, error) {
	registry := tools.NewDefaultRegistry()
	registry.SetDisabled(cfg.DisabledTools)
	servers := configuredMCPServers(cfg)
	if len(servers) == 0 {
		return registry, nil
	}
	if err := tools.RegisterMCPServers(ctx, registry, servers); err != nil {
		return nil, err
	}
	return registry, nil
}

func configuredMCPServers(cfg appconfig.Config) []tools.MCPServerConfig {
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]tools.MCPServerConfig, 0, len(names))
	for _, name := range names {
		server := cfg.MCPServers[name]
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		if strings.TrimSpace(server.Command) == "" {
			continue
		}
		servers = append(servers, tools.MCPServerConfig{
			Name:    name,
			Command: server.Command,
			Args:    append([]string(nil), server.Args...),
			Env:     server.Env,
		})
	}
	return servers
}

func setProviderBaseURL(cfg *appconfig.Config, provider, baseURL string) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "openai"
	}
	if cfg.BaseURLs == nil {
		cfg.BaseURLs = map[string]string{}
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		delete(cfg.BaseURLs, provider)
		return
	}
	cfg.BaseURLs[provider] = baseURL
}

func terminalTitleForProject(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd = "."
	}
	abs, err := filepath.Abs(cwd)
	if err == nil {
		cwd = abs
	}
	project := filepath.Base(filepath.Clean(cwd))
	if project == "." || project == string(filepath.Separator) || strings.TrimSpace(project) == "" {
		project = "workspace"
	}
	return "Klyra: " + project
}

func setTerminalTitle(out io.Writer, title string) {
	if strings.TrimSpace(title) == "" || os.Getenv("TERM") == "dumb" {
		return
	}
	file, ok := out.(*os.File)
	if !ok {
		return
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return
	}
	title = strings.NewReplacer("\x1b", "", "\x07", "", "\n", " ", "\r", " ").Replace(title)
	fmt.Fprintf(file, "\x1b]0;%s\x07", title)
}

// printJSONResult writes a JSON object with the agent result and usage to out.
func printJSONResult(out io.Writer, result agent.RunResult) error {
	payload := map[string]any{
		"result": result.Final,
		"usage": map[string]int{
			"input":     result.Usage.InputTokens,
			"cached":    result.Usage.CachedTokens,
			"output":    result.Usage.OutputTokens,
			"reasoning": result.Usage.ReasoningTokens,
			"total":     result.Usage.TotalTokens,
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(data))
	return nil
}

func loadEnvFile(dir string) {
	if dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, ".env")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			if len(val) >= 2 {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}
