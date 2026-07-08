package root

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/docker/cli/cli"
	"github.com/spf13/cobra"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider/dmr"
	"github.com/docker/docker-agent/pkg/telemetry"
	"github.com/docker/docker-agent/pkg/userconfig"
)

const dmrDocsURL = "https://docs.docker.com/ai/model-runner/get-started/"

const (
	dmrStatusReachable    = "reachable"
	dmrStatusNotInstalled = "not-installed"
	dmrStatusUnreachable  = "unreachable"
)

// doctorReport is the machine-readable form of the diagnosis. Secret values
// are never included, only where each one was found.
type doctorReport struct {
	Providers     []doctorProviderStatus `json:"providers"`
	DMR           doctorDMRStatus        `json:"dmr"`
	ModelsGateway string                 `json:"models_gateway,omitempty"`
	AutoModel     doctorAutoModel        `json:"auto_model"`
	AgentFile     *doctorAgentFileStatus `json:"agent_file,omitempty"`
	Issues        []string               `json:"issues,omitempty"`
}

type doctorProviderStatus struct {
	Provider string   `json:"provider"`
	EnvVars  []string `json:"env_vars"`
	Found    bool     `json:"found"`
	EnvVar   string   `json:"env_var,omitempty"`
	Source   string   `json:"source,omitempty"`
}

type doctorDMRStatus struct {
	Status string   `json:"status"`
	Error  string   `json:"error,omitempty"`
	Models []string `json:"models,omitempty"`
}

type doctorAutoModel struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	FromDefaultModel bool   `json:"from_default_model,omitempty"`
	Usable           bool   `json:"usable"`
	Note             string `json:"note,omitempty"`
}

type doctorAgentFileStatus struct {
	Ref            string                 `json:"ref"`
	Requirements   []doctorEnvRequirement `json:"requirements,omitempty"`
	ToolCheckError string                 `json:"tool_check_error,omitempty"`
}

type doctorEnvRequirement struct {
	EnvVar     string `json:"env_var"`
	RequiredBy string `json:"required_by"`
	Found      bool   `json:"found"`
	Source     string `json:"source,omitempty"`
}

type doctorFlags struct {
	jsonOutput bool
	runConfig  config.RuntimeConfig

	// Test seams: sourcesForTests replaces the env-file + default secret-source
	// chain, dmrLister replaces dmr.ListModels, and loadUserConfig replaces
	// userconfig.Load so tests never exec `pass`/`security`/`docker model` or
	// read the developer's real configuration.
	sourcesForTests []environment.Source
	dmrLister       config.DMRModelLister
	loadUserConfig  userConfigLoader
}

type doctorCmdOption func(*doctorFlags)

func newDoctorCmd(opts ...doctorCmdOption) *cobra.Command {
	var flags doctorFlags
	for _, opt := range opts {
		opt(&flags)
	}

	cmd := &cobra.Command{
		Use:   "doctor [agent-file]|[registry-ref]",
		Short: "Diagnose model and credential setup",
		Long: `Diagnose the model and credential setup.

Reports, without ever printing secret values:
  - which model providers have credentials and where each credential comes from
  - whether Docker Model Runner is reachable and which models are pulled
  - which model the 'auto' selection would pick
  - with an agent file: the environment variables it requires and their status

Exits with a non-zero status when an issue would prevent an agent from running.`,
		Example: `  docker-agent doctor
  docker-agent doctor ./agent.yaml
  docker-agent doctor --json`,
		Args:         cobra.MaximumNArgs(1),
		GroupID:      "core",
		SilenceUsage: true,
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeAlias(toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: flags.runDoctorCommand,
	}

	cmd.Flags().BoolVar(&flags.jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().StringSliceVar(&flags.runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")

	loadUserConfig := flags.loadUserConfig
	if loadUserConfig == nil {
		loadUserConfig = userconfig.Load
	}
	addGatewayFlags(cmd, &flags.runConfig, loadUserConfig)

	return cmd
}

func (f *doctorFlags) runDoctorCommand(cmd *cobra.Command, args []string) (commandErr error) {
	ctx := cmd.Context()
	telemetry.TrackCommand(ctx, "doctor", args)
	defer func() { // do not inline this defer so that commandErr is not resolved early
		telemetry.TrackCommandError(ctx, "doctor", args, commandErr)
	}()

	var agentRef string
	if len(args) == 1 {
		agentRef = args[0]
	}

	report, err := f.buildReport(ctx, agentRef)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if f.jsonOutput {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		printDoctorReport(w, report)
	}

	// A non-zero exit code makes the diagnosis scriptable (e.g. in CI).
	if n := len(report.Issues); n > 0 {
		return cli.StatusError{StatusCode: 1, Status: fmt.Sprintf("%d issue(s) found", n)}
	}

	return nil
}

func (f *doctorFlags) buildReport(ctx context.Context, agentRef string) (*doctorReport, error) {
	sources, err := f.secretSources()
	if err != nil {
		return nil, err
	}

	// Every lookup in the report goes through the same labeled source chain so
	// the provider table, the auto-selection result, and the agent-file checks
	// can never disagree with each other.
	providers := make([]environment.Provider, 0, len(sources))
	for _, source := range sources {
		providers = append(providers, source.Provider)
	}
	env := environment.NewMultiProvider(providers...)

	report := &doctorReport{ModelsGateway: f.runConfig.ModelsGateway}

	credFound := map[string]bool{}
	primaryEnvVar := map[string]string{}
	for _, p := range config.CloudProviderEnvVars() {
		status := doctorProviderStatus{Provider: p.Provider, EnvVars: p.EnvVars}
		for _, name := range p.EnvVars {
			if source, ok := findSource(ctx, sources, name); ok {
				status.Found, status.EnvVar, status.Source = true, name, source
				break
			}
		}
		credFound[p.Provider] = status.Found
		if len(p.EnvVars) > 0 {
			primaryEnvVar[p.Provider] = p.EnvVars[0]
		}
		report.Providers = append(report.Providers, status)
	}

	dmrModels, dmrErr := f.listDMRModels(ctx)
	switch {
	case dmrErr == nil:
		report.DMR = doctorDMRStatus{Status: dmrStatusReachable, Models: dmrModels}
	case errors.Is(dmrErr, dmr.ErrNotInstalled):
		report.DMR = doctorDMRStatus{Status: dmrStatusNotInstalled}
	default:
		report.DMR = doctorDMRStatus{Status: dmrStatusUnreachable, Error: dmrErr.Error()}
	}

	// Reuse the discovery results above instead of querying DMR a second time.
	lister := func(context.Context) ([]string, error) { return dmrModels, dmrErr }
	auto := config.AutoModelConfig(ctx, f.runConfig.ModelsGateway, env, f.runConfig.DefaultModel, lister)

	// Mirrors the condition under which AutoModelConfig short-circuits to the
	// configured default model.
	fromDefault := f.runConfig.DefaultModel != nil && f.runConfig.DefaultModel.Provider != "" && f.runConfig.DefaultModel.Model != ""

	autoStatus := doctorAutoModel{
		Provider:         auto.Provider,
		Model:            auto.Model,
		FromDefaultModel: fromDefault,
		Usable:           true,
	}

	switch {
	case f.runConfig.ModelsGateway != "":
		autoStatus.Note = "credentials are supplied by the models gateway"
		// Mirrors the run-time preflight: the Docker AI Gateway authenticates
		// with the Docker Desktop JWT, not per-provider API keys.
		if environment.IsTrustedDockerURL(f.runConfig.ModelsGateway) {
			if _, ok := findSource(ctx, sources, environment.DockerDesktopTokenEnv); !ok {
				autoStatus.Usable = false
				report.Issues = append(report.Issues,
					"the models gateway requires Docker Desktop sign-in and no DOCKER_TOKEN was found; sign in to Docker Desktop (check with `docker agent debug auth`)")
			}
		}

	case auto.Provider == "dmr":
		dmrDown := report.DMR.Status != dmrStatusReachable
		switch {
		case dmrDown && fromDefault:
			autoStatus.Usable = false
			report.Issues = append(report.Issues, fmt.Sprintf(
				"the configured default model %s/%s needs Docker Model Runner, which is %s; install or start it (%s)",
				auto.Provider, auto.Model, describeDMRStatus(report.DMR.Status), dmrDocsURL))
		case dmrDown:
			autoStatus.Usable = false
			report.Issues = append(report.Issues, fmt.Sprintf(
				"no usable model: no provider credential was found and Docker Model Runner is %s; run `docker agent setup`, or set an API key for one of the providers above (%s) or install Docker Model Runner (%s)",
				describeDMRStatus(report.DMR.Status), environment.SecretsDocsURL, dmrDocsURL))
		case !slices.Contains(dmrModels, auto.Model):
			autoStatus.Note = fmt.Sprintf("not pulled yet; run `docker model pull %s` or let the first run pull it", auto.Model)
		}

	default:
		// Only reachable through a configured default model: bare auto
		// selection never picks a cloud provider without credentials.
		if found, known := credFound[auto.Provider]; known && !found {
			autoStatus.Usable = false
			report.Issues = append(report.Issues, fmt.Sprintf(
				"the configured default model %s/%s has no credential for provider %s; %s (%s)",
				auto.Provider, auto.Model, auto.Provider, providerCredentialHint(auto.Provider, primaryEnvVar[auto.Provider]), environment.SecretsDocsURL))
		}
	}
	report.AutoModel = autoStatus

	if agentRef != "" {
		if err := f.checkAgentFile(ctx, agentRef, env, sources, report); err != nil {
			return nil, err
		}
	}

	return report, nil
}

// checkAgentFile reports the environment variables the agent configuration
// requires (models and tools), whether each one is set, and from which source.
func (f *doctorFlags) checkAgentFile(ctx context.Context, ref string, env environment.Provider, sources []environment.Source, report *doctorReport) error {
	agentSource, err := config.Resolve(ref, env)
	if err != nil {
		return err
	}

	cfg, err := config.Load(ctx, agentSource)
	if err != nil {
		return err
	}

	status := &doctorAgentFileStatus{Ref: ref}

	// first_available selectors resolve to the first candidate with
	// credentials; when none has any, surface it as an issue and keep
	// reporting the rest of the file's requirements. Only the error's first
	// line is kept: the full message repeats the secret-sources guidance.
	if err := config.ResolveFirstAvailableModels(ctx, cfg, f.runConfig.ModelsGateway, env); err != nil {
		firstLine, _, _ := strings.Cut(err.Error(), "\n")
		report.Issues = append(report.Issues, fmt.Sprintf("%s: %s", ref, firstLine))
	}

	requiredBy := map[string][]string{}
	for _, name := range config.RequiredModelEnvVars(ctx, cfg, f.runConfig.ModelsGateway, env) {
		requiredBy[name] = append(requiredBy[name], "models")
	}
	toolVars, toolErr := config.GatherEnvVarsForTools(ctx, cfg)
	if toolErr != nil {
		status.ToolCheckError = toolErr.Error()
	}
	for _, name := range toolVars {
		requiredBy[name] = append(requiredBy[name], "tools")
	}

	var missing []string
	for _, name := range slices.Sorted(maps.Keys(requiredBy)) {
		requirement := doctorEnvRequirement{EnvVar: name, RequiredBy: strings.Join(requiredBy[name], ", ")}
		if source, ok := findSource(ctx, sources, name); ok {
			requirement.Found = true
			requirement.Source = source
		} else {
			missing = append(missing, name)
		}
		status.Requirements = append(status.Requirements, requirement)
	}
	if len(missing) > 0 {
		report.Issues = append(report.Issues, fmt.Sprintf(
			"%s requires environment variables that are not set: %s (see %s)",
			ref, strings.Join(missing, ", "), environment.SecretsDocsURL))
	}

	report.AgentFile = status
	return nil
}

// secretSources returns the labeled secret sources in the same precedence
// order as the run-time env provider chain: --env-from-file first, then the
// default chain. EnvFiles were already made absolute and validated by the
// gateway-flags pre-run.
func (f *doctorFlags) secretSources() ([]environment.Source, error) {
	if f.sourcesForTests != nil {
		return f.sourcesForTests, nil
	}

	var sources []environment.Source
	if len(f.runConfig.EnvFiles) > 0 {
		envFiles, err := environment.NewEnvFilesProvider(f.runConfig.EnvFiles)
		if err != nil {
			return nil, fmt.Errorf("--env-from-file: %w", err)
		}
		sources = append(sources, environment.Source{Name: "env-file", Provider: envFiles})
	}

	return append(sources, environment.DefaultSources()...), nil
}

func (f *doctorFlags) listDMRModels(ctx context.Context) ([]string, error) {
	if f.dmrLister != nil {
		return f.dmrLister(ctx)
	}
	return dmr.ListModels(ctx)
}

// providerCredentialHint phrases the remediation for a missing provider
// credential: account-based providers point at the setup wizard's sign-in,
// the rest at their API-key env var.
func providerCredentialHint(provider, envVar string) string {
	if provider == chatgpt.ProviderName {
		return "sign in with `docker agent setup` (pick chatgpt) or set " + envVar
	}
	return "set " + envVar
}

// findSource returns the name of the first secret source that supplies a
// non-empty value for the variable. Empty values are skipped so a source that
// merely defines the variable (e.g. an env file with `KEY=`) is not reported
// as supplying a credential.
func findSource(ctx context.Context, sources []environment.Source, name string) (string, bool) {
	for _, source := range sources {
		if value, ok := source.Provider.Get(ctx, name); ok && value != "" {
			return source.Name, true
		}
	}
	return "", false
}

func describeDMRStatus(status string) string {
	if status == dmrStatusUnreachable {
		return "unreachable"
	}
	return "not installed"
}

func printDoctorReport(w io.Writer, report *doctorReport) {
	fmt.Fprintln(w, "Model provider credentials")
	tw := tabwriter.NewWriter(w, 0, 2, 3, ' ', 0)
	fmt.Fprintln(tw, "  PROVIDER\tSTATUS\tCREDENTIAL\tSOURCE")
	for _, p := range report.Providers {
		if p.Found {
			fmt.Fprintf(tw, "  %s\tfound\t%s\t%s\n", p.Provider, p.EnvVar, p.Source)
		} else {
			fmt.Fprintf(tw, "  %s\tnot set\t%s\t-\n", p.Provider, credentialCandidates(p.EnvVars))
		}
	}
	tw.Flush()

	fmt.Fprintln(w, "\nDocker Model Runner")
	switch report.DMR.Status {
	case dmrStatusReachable:
		if len(report.DMR.Models) == 0 {
			fmt.Fprintln(w, "  Status: reachable, no models pulled (run `docker model pull ai/qwen3` to get one)")
		} else {
			fmt.Fprintf(w, "  Status: reachable, %d model(s) pulled:\n", len(report.DMR.Models))
			for _, m := range report.DMR.Models {
				fmt.Fprintf(w, "    - %s\n", m)
			}
		}
	case dmrStatusNotInstalled:
		fmt.Fprintf(w, "  Status: not installed (%s)\n", dmrDocsURL)
	default:
		fmt.Fprintf(w, "  Status: unreachable: %s\n", report.DMR.Error)
		fmt.Fprintf(w, "  Install or start Docker Model Runner: %s\n", dmrDocsURL)
	}

	if report.ModelsGateway != "" {
		fmt.Fprintf(w, "\nModels gateway\n  %s\n", report.ModelsGateway)
	}

	fmt.Fprintln(w, "\nModel auto-selection")
	line := fmt.Sprintf("  auto -> %s/%s", report.AutoModel.Provider, report.AutoModel.Model)
	if report.AutoModel.FromDefaultModel {
		line += " (configured default model)"
	}
	fmt.Fprintln(w, line)
	if report.AutoModel.Note != "" {
		fmt.Fprintf(w, "  Note: %s\n", report.AutoModel.Note)
	}

	if af := report.AgentFile; af != nil {
		fmt.Fprintf(w, "\nAgent requirements: %s\n", af.Ref)
		if len(af.Requirements) == 0 {
			fmt.Fprintln(w, "  No environment variables required.")
		} else {
			tw := tabwriter.NewWriter(w, 0, 2, 3, ' ', 0)
			fmt.Fprintln(tw, "  ENV VAR\tREQUIRED BY\tSTATUS\tSOURCE")
			for _, r := range af.Requirements {
				if r.Found {
					fmt.Fprintf(tw, "  %s\t%s\tfound\t%s\n", r.EnvVar, r.RequiredBy, r.Source)
				} else {
					fmt.Fprintf(tw, "  %s\t%s\tnot set\t-\n", r.EnvVar, r.RequiredBy)
				}
			}
			tw.Flush()
		}
		if af.ToolCheckError != "" {
			fmt.Fprintf(w, "  Warning: %s\n", af.ToolCheckError)
		}
	}

	if len(report.Issues) == 0 {
		fmt.Fprintln(w, "\nNo issues found.")
		return
	}
	fmt.Fprintln(w, "\nIssues")
	for _, issue := range report.Issues {
		fmt.Fprintf(w, "  - %s\n", issue)
	}
}

// credentialCandidates renders the env vars that could supply a missing
// credential: the primary variable plus a count of the alternatives, keeping
// the table narrow for providers with several detection variables.
func credentialCandidates(envVars []string) string {
	switch len(envVars) {
	case 0:
		return "-"
	case 1:
		return envVars[0]
	default:
		return fmt.Sprintf("%s (+%d more)", envVars[0], len(envVars)-1)
	}
}
