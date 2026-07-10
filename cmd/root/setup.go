package root

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/cli"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/input"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/model/provider/dmr"
	"github.com/docker/docker-agent/pkg/telemetry"
	"github.com/docker/docker-agent/pkg/userconfig"
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
	// ProviderName and Provider are set when the custom path registered an
	// OpenAI-compatible provider in the user config.
	ProviderName string
	Provider     *latest.ProviderConfig
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
	saveProvider func(name string, provider latest.ProviderConfig) error
	listModels   func(ctx context.Context, baseURL, token string) []string
}

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactively set up a model (API key, local, or custom endpoint)",
		Long: `Set up a model for docker agent, interactively.

Three paths:
  - Cloud provider: pick a provider, paste its API key, and choose where to
    store it (OS keychain, pass, or the docker agent env file). Picking
    chatgpt signs in with your ChatGPT account in the browser instead of
    asking for an API key.
  - Local model: check Docker Model Runner and pull a model. No API key needed.
  - OpenAI-compatible provider: register a custom endpoint (vLLM, LiteLLM,
    a corporate gateway, ...) with its API format and API key variable. The
    provider is saved to your user configuration and its models become
    usable everywhere via --model <name>/<model>.

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
		saveProvider: func(name string, provider latest.ProviderConfig) error {
			return userconfig.Update(func(cfg *userconfig.Config) error {
				return cfg.SetProvider(name, provider)
			})
		},
		listModels: fetchOpenAICompatibleModels,
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
	fmt.Fprintln(w.out, "  3. OpenAI-compatible provider (custom endpoint, e.g. vLLM, LiteLLM)")

	choice, err := w.promptChoice(ctx, 3, 1)
	if err != nil {
		return nil, err
	}

	var result *setupResult
	switch choice {
	case 1:
		result, err = w.setupCloudProvider(ctx)
	case 2:
		result, err = w.setupLocalModel(ctx)
	default:
		result, err = w.setupCustomProvider(ctx)
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

// customAPITypes maps the wizard's API-format menu entries to the api_type
// values understood by the providers section.
var customAPITypes = []string{"openai_chatcompletions", "openai_responses"}

// setupCustomProvider walks the custom path: name an OpenAI-compatible
// provider, point it at an endpoint, pick the API format, optionally wire an
// API key, and save the provider to the user config so every command can use
// its models via --model <name>/<model>.
func (w *setupWizard) setupCustomProvider(ctx context.Context) (*setupResult, error) {
	name, err := w.promptProviderName(ctx)
	if err != nil {
		return nil, err
	}

	baseURL, err := w.promptBaseURL(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "Which API format does the endpoint use?")
	fmt.Fprintln(w.out, "  1. Chat Completions (/chat/completions, most common)")
	fmt.Fprintln(w.out, "  2. Responses (/responses)")
	formatChoice, err := w.promptChoice(ctx, len(customAPITypes), 1)
	if err != nil {
		return nil, err
	}

	envVar, key, err := w.promptCustomProviderKey(ctx, name)
	if err != nil {
		return nil, err
	}

	providerCfg := latest.ProviderConfig{
		BaseURL:  baseURL,
		APIType:  customAPITypes[formatChoice-1],
		TokenKey: envVar,
	}

	model, err := w.promptCustomProviderModel(ctx, baseURL, key)
	if err != nil {
		return nil, err
	}

	if err := w.saveProvider(name, providerCfg); err != nil {
		return nil, fmt.Errorf("saving provider %q to the user config: %w", name, err)
	}
	fmt.Fprintf(w.out, "\nSaved provider %q (%s) to %s.\n", name, baseURL, userconfig.Path())

	result := &setupResult{EnvVar: envVar, Value: key, ProviderName: name, Provider: &providerCfg}
	if model != "" {
		result.Model = name + "/" + model
	}
	return result, nil
}

// promptProviderName asks for the custom provider's name until a valid,
// non-conflicting one is entered. Built-in provider names are rejected so a
// custom definition never silently shadows them.
func (w *setupWizard) promptProviderName(ctx context.Context) (string, error) {
	for {
		fmt.Fprint(w.out, "\nProvider name (e.g. myprovider): ")
		name, err := w.readLine(ctx)
		if err != nil {
			return "", err
		}
		name = strings.TrimSpace(name)
		switch {
		case name == "":
			fmt.Fprintln(w.out, "The name is empty; enter one or press Ctrl+C to cancel.")
		case strings.ContainsAny(name, "/ \t"):
			fmt.Fprintln(w.out, "The name cannot contain '/' or whitespace.")
		case name == "auto" || provider.IsKnownProvider(name):
			fmt.Fprintf(w.out, "%q is a built-in provider name; pick another one.\n", name)
		default:
			return name, nil
		}
	}
}

// promptBaseURL asks for the endpoint's base URL until an absolute http(s)
// URL is entered.
func (w *setupWizard) promptBaseURL(ctx context.Context) (string, error) {
	for {
		fmt.Fprint(w.out, "Base URL of the API endpoint (e.g. https://api.example.com/v1): ")
		baseURL, err := w.readLine(ctx)
		if err != nil {
			return "", err
		}
		baseURL = strings.TrimSpace(baseURL)
		u, parseErr := url.Parse(baseURL)
		if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			fmt.Fprintln(w.out, "Enter an absolute http(s) URL, e.g. https://api.example.com/v1.")
			continue
		}
		return baseURL, nil
	}
}

// promptCustomProviderKey asks which environment variable holds the API key
// (empty for an unauthenticated endpoint) and, when one is named, collects
// and stores the key like the cloud path does. The raw key is also returned
// so the wizard can authenticate its model-discovery request.
func (w *setupWizard) promptCustomProviderKey(ctx context.Context, name string) (envVar, key string, err error) {
	for {
		fmt.Fprint(w.out, "Environment variable that holds the API key (empty if none is needed): ")
		envVar, err = w.readLine(ctx)
		if err != nil {
			return "", "", err
		}
		envVar = strings.TrimSpace(envVar)
		if envVar == "" {
			return "", "", nil
		}
		if !isValidEnvVarName(envVar) {
			fmt.Fprintln(w.out, "Enter a valid environment variable name (letters, digits and underscores, e.g. MYPROVIDER_API_KEY), or leave it empty.")
			continue
		}
		break
	}

	key, err = w.promptSecret(ctx, fmt.Sprintf("\nPaste your %s API key (%s, input hidden): ", name, envVar))
	if err != nil {
		return "", "", err
	}
	if err := w.storeSecret(ctx, envVar, key); err != nil {
		return "", "", err
	}
	return envVar, key, nil
}

// promptCustomProviderModel queries the endpoint for its models to validate
// the configuration and suggest a default, then asks which model to use.
// Discovery failures are not fatal: the user can type a model ID manually or
// leave it empty and discover models later with `docker agent models`.
func (w *setupWizard) promptCustomProviderModel(ctx context.Context, baseURL, key string) (string, error) {
	fmt.Fprintln(w.out)
	fmt.Fprintln(w.out, "Checking the endpoint for available models...")

	models := w.listModels(ctx, baseURL, key)
	var defaultModel string
	if len(models) > 0 {
		fmt.Fprintf(w.out, "The endpoint lists %d model(s):\n", len(models))
		for i, m := range models {
			if i == maxModelsShown {
				fmt.Fprintf(w.out, "  ... and %d more (see `docker agent models`)\n", len(models)-maxModelsShown)
				break
			}
			fmt.Fprintf(w.out, "  - %s\n", m)
		}
		defaultModel = models[0]
	} else {
		fmt.Fprintln(w.out, "Could not list models from the endpoint; you can enter one manually.")
	}

	if defaultModel != "" {
		fmt.Fprintf(w.out, "Model to use [%s]: ", defaultModel)
	} else {
		fmt.Fprint(w.out, "Model to use (leave empty to pick one later): ")
	}
	model, err := w.readLine(ctx)
	if err != nil {
		return "", err
	}
	if model = strings.TrimSpace(model); model == "" {
		model = defaultModel
	}
	return model, nil
}

// maxModelsShown caps the endpoint's discovered-model listing so a provider
// serving hundreds of models does not flood the wizard.
const maxModelsShown = 10

// envVarNameRe matches portable environment variable names.
var envVarNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isValidEnvVarName(name string) bool {
	return envVarNameRe.MatchString(name)
}

// printNextSteps ends the wizard with ready-to-copy commands.
func (w *setupWizard) printNextSteps(result *setupResult) {
	fmt.Fprintln(w.out)

	// Auto-selection never picks a custom provider, so its next steps must
	// carry the explicit --model reference (or how to find one) instead of
	// the bare `docker agent run`.
	if result.ProviderName != "" {
		if result.Model != "" {
			fmt.Fprintln(w.out, "You're all set. Start chatting with:")
			fmt.Fprintln(w.out)
			fmt.Fprintf(w.out, "  docker agent run --model %s\n", result.Model)
		} else {
			fmt.Fprintf(w.out, "Provider %q is set up. List its models with:\n", result.ProviderName)
			fmt.Fprintln(w.out)
			fmt.Fprintf(w.out, "  docker agent models --provider %s\n", result.ProviderName)
			fmt.Fprintln(w.out)
			fmt.Fprintln(w.out, "Then start chatting with:")
			fmt.Fprintln(w.out)
			fmt.Fprintf(w.out, "  docker agent run --model %s/<model>\n", result.ProviderName)
		}
		fmt.Fprintln(w.out)
		fmt.Fprintln(w.out, "Check your setup anytime with `docker agent doctor`.")
		return
	}

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
//
//nolint:unparam // def is 1 for every current menu; the parameter documents the default-choice contract
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

	// A provider registered by the wizard was saved after the run config was
	// seeded from the user config, so bridge it in for the retry too. Auto
	// model selection never picks a custom provider, so pin the model chosen
	// in the wizard unless a default model is already configured.
	if result.ProviderName != "" && result.Provider != nil {
		if f.runConfig.Providers == nil {
			f.runConfig.Providers = map[string]latest.ProviderConfig{}
		}
		f.runConfig.Providers[result.ProviderName] = *result.Provider
		if result.Model != "" && f.runConfig.DefaultModel == nil {
			f.runConfig.DefaultModel = parseModelShorthand(result.Model)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Retrying with the new configuration...")
	return f.runOrExec(ctx, out, args, useTUI)
}
