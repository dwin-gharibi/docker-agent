package root

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/cli"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/input"
	"github.com/docker/docker-agent/pkg/model/provider/dmr"
	"github.com/docker/docker-agent/pkg/telemetry"
)

// errSetupCancelled is returned when the user aborts the wizard (EOF or an
// explicit quit) rather than a step failing.
var errSetupCancelled = errors.New("setup cancelled")

// errNoUsableModel is the concise error returned when the setup offer is
// declined or cancelled: the full failure was already printed just above the
// offer, so returning the original error would print it twice.
var errNoUsableModel = errors.New("no usable model is configured; run `docker agent setup` or see `docker agent doctor`")

// setupResult reports what the wizard configured, so the caller that offered
// setup after a failed run can retry with the new credential in place.
type setupResult struct {
	// EnvVar and Value are set when the cloud path stored an API key.
	EnvVar string
	Value  string
	// Model is set when the local path selected or pulled a DMR model.
	Model string
}

// setupWizard drives the interactive model setup. The function fields are
// seams: production wiring talks to the terminal, the OS secret stores, and
// Docker Model Runner, while tests inject scripted answers and fakes.
//
// in is buffered once at construction: a fresh bufio.Reader per prompt would
// drop the read-ahead it buffered beyond the first line.
type setupWizard struct {
	in  *bufio.Reader
	out io.Writer

	readSecret   func(prompt string) (string, error)
	stores       []environment.SecretStore
	dmrLister    config.DMRModelLister
	pullModel    func(ctx context.Context, model string) error
	chatgptLogin func(ctx context.Context, out io.Writer) (*chatgpt.LoginResult, error)
}

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactively set up a model (API key or local)",
		Long: `Set up a model for docker agent, interactively.

Two paths:
  - Cloud provider: pick a provider, paste its API key, and choose where to
    store it (OS keychain, pass, or the docker agent env file). Picking
    chatgpt signs in with your ChatGPT account in the browser instead of
    asking for an API key.
  - Local model: check Docker Model Runner and pull a model. No API key needed.

Ends with the exact command to start chatting. Secret values are stored where
you choose and never printed. Check the result anytime with 'docker agent doctor'.`,
		Example:      `  docker-agent setup`,
		GroupID:      "core",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) (commandErr error) {
			ctx := cmd.Context()
			telemetry.TrackCommand(ctx, "setup", args)
			defer func() { // do not inline this defer so that commandErr is not resolved early
				telemetry.TrackCommandError(ctx, "setup", args, commandErr)
			}()

			if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
				return fmt.Errorf("docker agent setup is interactive and needs a terminal\n"+
					"Without one, set a provider API key directly (e.g. export ANTHROPIC_API_KEY=<value>)\n"+
					"or pull a local model with `docker model pull ai/qwen3`.\n"+
					"See %s for every secret source", environment.SecretsDocsURL)
			}

			wizard := newTerminalSetupWizard(cmd.InOrStdin(), cmd.OutOrStdout())
			_, err := wizard.run(ctx)
			if errors.Is(err, errSetupCancelled) {
				fmt.Fprintln(cmd.OutOrStdout(), "Setup cancelled.")
				return nil
			}
			return err
		},
	}
}

// newTerminalSetupWizard wires a wizard to the real terminal, secret stores,
// and Docker Model Runner.
func newTerminalSetupWizard(in io.Reader, out io.Writer) *setupWizard {
	return &setupWizard{
		in:  bufio.NewReader(in),
		out: out,
		readSecret: func(prompt string) (string, error) {
			fmt.Fprint(out, prompt)
			// Read directly from the terminal fd: the key must not echo.
			value, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(out)
			if err != nil {
				return "", fmt.Errorf("reading the API key: %w", err)
			}
			return string(value), nil
		},
		stores:       environment.SecretStores(),
		dmrLister:    dmr.ListModels,
		pullModel:    dmr.Pull,
		chatgptLogin: chatgpt.Login,
	}
}

// run executes the wizard: choose cloud or local, configure it, and print the
// command to start chatting.
func (w *setupWizard) run(ctx context.Context) (*setupResult, error) {
	fmt.Fprintln(w.out, "Let's set up a model for docker agent.")
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "How do you want to run models?")
	fmt.Fprintln(w.out, "  1. Cloud provider (needs an API key)")
	fmt.Fprintln(w.out, "  2. Local model via Docker Model Runner (no API key)")

	choice, err := w.promptChoice(ctx, 2, 1)
	if err != nil {
		return nil, err
	}

	var result *setupResult
	if choice == 1 {
		result, err = w.setupCloudProvider(ctx)
	} else {
		result, err = w.setupLocalModel(ctx)
	}
	if err != nil {
		return nil, err
	}

	w.printNextSteps(result)
	return result, nil
}

// setupCloudProvider walks the cloud path: pick a provider, paste its key,
// pick a store, and persist the key there.
func (w *setupWizard) setupCloudProvider(ctx context.Context) (*setupResult, error) {
	providers := config.CloudProviderEnvVars()

	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "Pick a provider:")
	for i, p := range providers {
		credential := p.EnvVars[0]
		if p.Provider == chatgpt.ProviderName {
			credential = "ChatGPT account sign-in, no API key"
		}
		fmt.Fprintf(w.out, "  %2d. %-15s (%s)\n", i+1, p.Provider, credential)
	}

	choice, err := w.promptChoice(ctx, len(providers), 1)
	if err != nil {
		return nil, err
	}
	selected := providers[choice-1]
	envVar := selected.EnvVars[0]

	// The chatgpt provider signs in with a browser flow instead of a pasted
	// API key; the credential is stored by the login itself.
	if selected.Provider == chatgpt.ProviderName {
		fmt.Fprintln(w.out)
		result, err := w.chatgptLogin(ctx, w.out)
		if err != nil {
			return nil, err
		}
		if result.Email != "" {
			fmt.Fprintf(w.out, "Signed in as %s.\n", result.Email)
		} else {
			fmt.Fprintln(w.out, "Signed in with your ChatGPT account.")
		}
		return &setupResult{Model: selected.Provider + "/" + config.DefaultModels[selected.Provider]}, nil
	}

	key, err := w.promptSecret(ctx, fmt.Sprintf("\nPaste your %s API key (%s, input hidden): ", selected.Provider, envVar))
	if err != nil {
		return nil, err
	}

	if err := w.storeSecret(ctx, envVar, key); err != nil {
		return nil, err
	}

	return &setupResult{EnvVar: envVar, Value: key, Model: selected.Provider + "/" + config.DefaultModels[selected.Provider]}, nil
}

// promptSecret asks for the API key until a non-empty value is entered.
func (w *setupWizard) promptSecret(ctx context.Context, prompt string) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		key, err := w.readSecret(prompt)
		if err != nil {
			return "", err
		}
		if key = strings.TrimSpace(key); key != "" {
			return key, nil
		}
		fmt.Fprintln(w.out, "The key is empty; paste it or press Ctrl+C to cancel.")
	}
}

// storeSecret asks where to store the key and persists it, re-asking when a
// store fails (e.g. an uninitialized pass store) so the pasted key is not
// lost to a storage hiccup.
func (w *setupWizard) storeSecret(ctx context.Context, envVar, key string) error {
	for {
		fmt.Fprintln(w.out)
		fmt.Fprintf(w.out, "Where should %s be stored?\n", envVar)
		for i, store := range w.stores {
			fmt.Fprintf(w.out, "  %d. %s\n", i+1, store.Description())
		}

		choice, err := w.promptChoice(ctx, len(w.stores), 1)
		if err != nil {
			return err
		}
		store := w.stores[choice-1]

		if err := store.Store(ctx, envVar, key); err != nil {
			fmt.Fprintf(w.out, "Could not store the key: %v\nPick another location, or press Ctrl+C to cancel.\n", err)
			continue
		}

		fmt.Fprintf(w.out, "\nStored %s in the %s.\n", envVar, store.Description())
		return nil
	}
}

// setupLocalModel walks the local path: check Docker Model Runner and make
// sure at least one model is pulled.
func (w *setupWizard) setupLocalModel(ctx context.Context) (*setupResult, error) {
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "Checking Docker Model Runner...")

	models, err := w.dmrLister(ctx)
	switch {
	case errors.Is(err, dmr.ErrNotInstalled):
		return nil, fmt.Errorf("cannot use a local model: Docker Model Runner is not installed.\n"+
			"Install it (%s), then run `docker agent setup` again", dmrDocsURL)
	case err != nil:
		return nil, fmt.Errorf("cannot use a local model: Docker Model Runner is not reachable: %w\n"+
			"Start it (or install it: %s), then run `docker agent setup` again", err, dmrDocsURL)
	}

	if len(models) > 0 {
		fmt.Fprintf(w.out, "Docker Model Runner is ready with %d model(s) pulled:\n", len(models))
		for _, m := range models {
			fmt.Fprintf(w.out, "  - %s\n", m)
		}
		model, _ := config.PickDMRModel(ctx, config.DefaultModels["dmr"], func(context.Context) ([]string, error) { return models, nil })
		return &setupResult{Model: "dmr/" + model}, nil
	}

	defaultModel := config.DefaultModels["dmr"]
	fmt.Fprintln(w.out, "Docker Model Runner is reachable but no model is pulled yet.")
	fmt.Fprintf(w.out, "Model to pull [%s]: ", defaultModel)

	model, err := w.readLine(ctx)
	if err != nil {
		return nil, err
	}
	if model = strings.TrimSpace(model); model == "" {
		model = defaultModel
	}

	if err := w.pullModel(ctx, model); err != nil {
		return nil, err
	}

	return &setupResult{Model: "dmr/" + model}, nil
}

// printNextSteps ends the wizard with ready-to-copy commands.
func (w *setupWizard) printNextSteps(result *setupResult) {
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "You're all set. Start chatting with:")
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "  docker agent run")
	fmt.Fprintln(w.out)
	if result.Model != "" {
		fmt.Fprintf(w.out, "Or pin the model explicitly: docker agent run --model %s\n", result.Model)
	}
	fmt.Fprintln(w.out, "Check your setup anytime with `docker agent doctor`.")
}

// promptChoice reads a 1-based menu choice, re-asking on invalid input. An
// empty answer selects def; EOF cancels the wizard.
func (w *setupWizard) promptChoice(ctx context.Context, n, def int) (int, error) {
	for {
		fmt.Fprintf(w.out, "Choice [%d]: ", def)
		answer, err := w.readLine(ctx)
		if err != nil {
			return 0, err
		}
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return def, nil
		}
		if choice, err := strconv.Atoi(answer); err == nil && choice >= 1 && choice <= n {
			return choice, nil
		}
		fmt.Fprintf(w.out, "Enter a number between 1 and %d.\n", n)
	}
}

// readLine reads one line of user input, mapping EOF (Ctrl+D, closed stdin)
// and context cancellation (Ctrl+C) to a cancellation.
func (w *setupWizard) readLine(ctx context.Context) (string, error) {
	line, err := input.ReadLine(ctx, w.in)
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return "", errSetupCancelled
	}
	if err != nil {
		return "", err
	}
	return line, nil
}

// noSetupOfferEnvVars suppress the automatic setup offer for scripted
// environments driving a real terminal (mirrors the tour's NO_TOUR escape).
var noSetupOfferEnvVars = []string{"DOCKER_AGENT_NO_SETUP", "CAGENT_NO_SETUP"}

func setupOfferDisabledByEnv(getenv func(string) string) bool {
	for _, name := range noSetupOfferEnvVars {
		if getenv(name) == "1" {
			return true
		}
	}
	return false
}

// errorIndicatesNoUsableModel reports whether err means "no usable model or
// missing model credentials", the failures the setup wizard fixes. Errors
// that already name their own exact remediation (a failed or declined pull of
// a specific model) are excluded: re-offering a generic wizard on top of them
// would drown the fix they carry.
func errorIndicatesNoUsableModel(err error) bool {
	if _, ok := errors.AsType[*config.AutoModelFallbackError](err); ok {
		return true
	}
	if errors.Is(err, dmr.ErrNotInstalled) {
		return true
	}
	if reqErr, ok := errors.AsType[*environment.RequiredEnvError](err); ok {
		return reqErr.MissingModelCredentials
	}
	// Matches errors that self-classify, e.g. the unexported first_available
	// variant in pkg/config.
	var modelCreds interface{ MissingModelCredentials() bool }
	if errors.As(err, &modelCreds) {
		return modelCreds.MissingModelCredentials()
	}
	return false
}

// shouldOfferSetup reports whether a failed run should offer the setup
// wizard: interactive terminal on both ends, not exec mode, not suppressed
// via environment, and a failure the wizard can actually fix.
func shouldOfferSetup(runErr error, execMode bool, getenv func(string) string) bool {
	if runErr == nil || execMode || setupOfferDisabledByEnv(getenv) {
		return false
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
		return false
	}
	return errorIndicatesNoUsableModel(runErr)
}

// offerSetupOnNoModel completes an interactive run that failed for lack of a
// usable model: it surfaces the failure, offers the setup wizard (decline-able),
// and retries the run once when setup succeeds. In every other case the
// original error is returned unchanged.
func (f *runExecFlags) offerSetupOnNoModel(ctx context.Context, cmd *cobra.Command, out *cli.Printer, args []string, useTUI bool, runErr error) error {
	if !shouldOfferSetup(runErr, f.exec, os.Getenv) {
		return runErr
	}

	errOut := cmd.ErrOrStderr()
	fmt.Fprintf(errOut, "%v\n\n", runErr)
	fmt.Fprint(errOut, "Run the interactive setup now to configure a model? ([y]es/[n]o): ")

	answer, err := input.ReadLine(ctx, cmd.InOrStdin())
	if err != nil {
		fmt.Fprintln(errOut)
		return errNoUsableModel
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		return errNoUsableModel
	}

	fmt.Fprintln(cmd.OutOrStdout())
	wizard := newTerminalSetupWizard(cmd.InOrStdin(), cmd.OutOrStdout())
	result, err := wizard.run(ctx)
	if errors.Is(err, errSetupCancelled) {
		return errNoUsableModel
	}
	if err != nil {
		return err
	}

	// The run's env provider chain was built before the wizard stored the key,
	// so bridge it into the process environment for the retry. Keychain and
	// pass lookups are live either way; the config env file is not, when it
	// did not exist at chain construction.
	if result.EnvVar != "" {
		if err := os.Setenv(result.EnvVar, result.Value); err != nil {
			slog.WarnContext(ctx, "Failed to export the stored key for the retry", "env_var", result.EnvVar, "error", err)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Retrying with the new configuration...")
	return f.runOrExec(ctx, out, args, useTUI)
}
