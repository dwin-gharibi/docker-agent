package root

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/cli/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider/dmr"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// withDoctorTestEnv wires a hermetic doctor command: a map-backed secret
// source chain, a stubbed DMR lister, and an empty user config, so tests
// never exec `pass`/`security`/`docker model` and never read the developer's
// real configuration.
func withDoctorTestEnv(env map[string]string, dmrModels []string, dmrErr error) doctorCmdOption {
	return func(f *doctorFlags) {
		mapProvider := environment.NewMapEnvProvider(env)
		f.runConfig.EnvProviderForTests = mapProvider
		f.sourcesForTests = []environment.Source{{Name: "environment", Provider: mapProvider}}
		f.dmrLister = func(context.Context) ([]string, error) { return dmrModels, dmrErr }
		f.loadUserConfig = func() (*userconfig.Config, error) { return &userconfig.Config{}, nil }
	}
}

func executeDoctor(t *testing.T, args []string, opts ...doctorCmdOption) (string, error) {
	t.Helper()

	var buf bytes.Buffer
	cmd := newDoctorCmd(opts...)
	cmd.SilenceErrors = true
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

func TestDoctorCommand_ReportsCredentialSource(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil, withDoctorTestEnv(
		map[string]string{"ANTHROPIC_API_KEY": "sk-secret-value"},
		[]string{"ai/qwen3:latest"}, nil))

	require.NoError(t, err)
	assert.Regexp(t, `anthropic\s+found\s+ANTHROPIC_API_KEY\s+environment`, output)
	assert.Regexp(t, `openai\s+not set\s+OPENAI_API_KEY\s+-`, output)
	assert.Contains(t, output, "auto -> anthropic/claude-sonnet-4-6")
	assert.Contains(t, output, "No issues found.")
	assert.NotContains(t, output, "sk-secret-value", "secret values must never be printed")
}

func TestDoctorCommand_SourcePrecedenceMatchesProviderChain(t *testing.T) {
	t.Parallel()

	fileProvider := environment.NewMapEnvProvider(map[string]string{"OPENAI_API_KEY": "from-file"})
	osProvider := environment.NewMapEnvProvider(map[string]string{"OPENAI_API_KEY": "from-env"})

	output, err := executeDoctor(t, nil, func(f *doctorFlags) {
		f.runConfig.EnvProviderForTests = environment.NewMultiProvider(fileProvider, osProvider)
		f.sourcesForTests = []environment.Source{
			{Name: "env-file", Provider: fileProvider},
			{Name: "environment", Provider: osProvider},
		}
		f.dmrLister = func(context.Context) ([]string, error) { return nil, nil }
		f.loadUserConfig = func() (*userconfig.Config, error) { return &userconfig.Config{}, nil }
	})

	require.NoError(t, err)
	assert.Regexp(t, `openai\s+found\s+OPENAI_API_KEY\s+env-file`, output)
}

func TestDoctorCommand_ChatGPTLoginSource(t *testing.T) {
	t.Parallel()

	// The chatgpt-login source serves the virtual CHATGPT_OAUTH_TOKEN
	// variable from the stored browser sign-in.
	login := environment.NewMapEnvProvider(map[string]string{"CHATGPT_OAUTH_TOKEN": "chatgpt-access-token"})

	output, err := executeDoctor(t, nil, func(f *doctorFlags) {
		f.runConfig.EnvProviderForTests = login
		f.sourcesForTests = []environment.Source{
			{Name: "environment", Provider: environment.NewMapEnvProvider(nil)},
			{Name: "chatgpt-login", Provider: login},
		}
		f.dmrLister = func(context.Context) ([]string, error) { return []string{"ai/qwen3:latest"}, nil }
		f.loadUserConfig = func() (*userconfig.Config, error) { return &userconfig.Config{}, nil }
	})

	require.NoError(t, err)
	assert.Regexp(t, `chatgpt\s+found\s+CHATGPT_OAUTH_TOKEN\s+chatgpt-login`, output)
	assert.Contains(t, output, "auto -> chatgpt/gpt-5.6")
	assert.Contains(t, output, "No issues found.")
	assert.NotContains(t, output, "chatgpt-access-token", "secret values must never be printed")
}

func TestDoctorCommand_ChatGPTDefaultModelWithoutLoginSuggestsSignIn(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil,
		withDoctorTestEnv(nil, []string{"ai/qwen3:latest"}, nil),
		func(f *doctorFlags) {
			f.runConfig.DefaultModel = &latest.ModelConfig{Provider: "chatgpt", Model: "gpt-5.2"}
		})

	require.Error(t, err, "a default model without credentials is an issue")
	assert.Regexp(t, `chatgpt\s+not set\s+CHATGPT_OAUTH_TOKEN`, output)
	assert.Contains(t, output, "sign in with `docker agent setup` (pick chatgpt) or set CHATGPT_OAUTH_TOKEN")
}

func TestDoctorCommand_EmptyValueIsNotACredential(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil, withDoctorTestEnv(
		map[string]string{"OPENAI_API_KEY": "", "MISTRAL_API_KEY": "key"},
		[]string{"ai/qwen3:latest"}, nil))

	require.NoError(t, err)
	assert.Regexp(t, `openai\s+not set`, output)
	assert.Regexp(t, `mistral\s+found\s+MISTRAL_API_KEY\s+environment`, output)
}

func TestDoctorCommand_NoUsableModel(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil, withDoctorTestEnv(nil, nil, dmr.ErrNotInstalled))

	require.Error(t, err)
	statusErr, ok := errors.AsType[cli.StatusError](err)
	require.True(t, ok, "expected a cli.StatusError, got %T", err)
	assert.Equal(t, 1, statusErr.StatusCode)

	assert.Contains(t, output, "Status: not installed")
	assert.Contains(t, output, "Issues")
	assert.Contains(t, output, "no usable model")
	assert.Contains(t, output, environment.SecretsDocsURL)
	assert.Contains(t, output, dmrDocsURL)
}

func TestDoctorCommand_DMRUnreachable(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil, withDoctorTestEnv(nil, nil, errors.New("connection refused")))

	require.Error(t, err)
	assert.Contains(t, output, "Status: unreachable: connection refused")
	assert.Contains(t, output, "no usable model")
}

func TestDoctorCommand_DMRModelNotPulled(t *testing.T) {
	t.Parallel()

	// DMR reachable with no models pulled: the run would offer a pull, so it
	// is a note on the selection, not an issue.
	output, err := executeDoctor(t, nil, withDoctorTestEnv(nil, nil, nil))

	require.NoError(t, err)
	assert.Contains(t, output, "auto -> dmr/ai/qwen3:latest")
	assert.Contains(t, output, "docker model pull ai/qwen3:latest")
	assert.Contains(t, output, "No issues found.")
}

func TestDoctorCommand_DMRPrefersLocalModel(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil, withDoctorTestEnv(nil, []string{"ai/smollm2"}, nil))

	require.NoError(t, err)
	assert.Contains(t, output, "auto -> dmr/ai/smollm2")
	assert.Contains(t, output, "- ai/smollm2")
	assert.Contains(t, output, "No issues found.")
}

func TestDoctorCommand_InvalidUserConfig(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil,
		withDoctorTestEnv(map[string]string{"ANTHROPIC_API_KEY": "sk-secret"}, []string{"ai/qwen3:latest"}, nil),
		func(f *doctorFlags) {
			f.loadUserConfig = func() (*userconfig.Config, error) {
				return nil, errors.New("failed to parse config file")
			}
		})

	require.Error(t, err)
	assert.Contains(t, output, "User configuration")
	assert.Contains(t, output, "cannot be parsed")
}

func TestDoctorCommand_DefaultModelWithoutCredential(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, nil, func(f *doctorFlags) {
		mapProvider := environment.NewMapEnvProvider(nil)
		f.runConfig.EnvProviderForTests = mapProvider
		f.sourcesForTests = []environment.Source{{Name: "environment", Provider: mapProvider}}
		f.dmrLister = func(context.Context) ([]string, error) { return []string{"ai/qwen3:latest"}, nil }
		f.loadUserConfig = func() (*userconfig.Config, error) {
			return &userconfig.Config{
				DefaultModel: &latest.FlexibleModelConfig{
					ModelConfig: latest.ModelConfig{Provider: "openai", Model: "gpt-5"},
				},
			}, nil
		}
	})

	require.Error(t, err)
	assert.Contains(t, output, "auto -> openai/gpt-5 (configured default model)")
	assert.Contains(t, output, "no credential for provider openai")
	assert.Contains(t, output, "OPENAI_API_KEY")
}

func TestDoctorCommand_ModelsGateway(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, []string{"--models-gateway", "https://gateway.example.com"},
		withDoctorTestEnv(nil, nil, dmr.ErrNotInstalled))

	require.NoError(t, err)
	assert.Contains(t, output, "https://gateway.example.com")
	assert.Contains(t, output, "credentials are supplied by the models gateway")
	assert.Contains(t, output, "No issues found.")
}

func TestDoctorCommand_DockerGatewayNeedsSignIn(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, []string{"--models-gateway", "https://api.docker.com/gateway"},
		withDoctorTestEnv(nil, nil, dmr.ErrNotInstalled))

	require.Error(t, err)
	assert.Contains(t, output, "requires Docker Desktop sign-in")

	output, err = executeDoctor(t, []string{"--models-gateway", "https://api.docker.com/gateway"},
		withDoctorTestEnv(map[string]string{"DOCKER_TOKEN": "jwt"}, nil, dmr.ErrNotInstalled))

	require.NoError(t, err)
	assert.Contains(t, output, "No issues found.")
}

func TestDoctorCommand_JSON(t *testing.T) {
	t.Parallel()

	output, err := executeDoctor(t, []string{"--json"}, withDoctorTestEnv(
		map[string]string{"OPENAI_API_KEY": "sk-json-secret"},
		[]string{"ai/qwen3:latest"}, nil))

	require.NoError(t, err)
	assert.NotContains(t, output, "sk-json-secret", "secret values must never be printed")

	var report doctorReport
	require.NoError(t, json.Unmarshal([]byte(output), &report))

	assert.Equal(t, dmrStatusReachable, report.DMR.Status)
	assert.Equal(t, []string{"ai/qwen3:latest"}, report.DMR.Models)
	assert.Equal(t, "openai", report.AutoModel.Provider)
	assert.Equal(t, "gpt-5.6", report.AutoModel.Model)
	assert.True(t, report.AutoModel.Usable)
	assert.Empty(t, report.Issues)

	byProvider := map[string]doctorProviderStatus{}
	for _, p := range report.Providers {
		byProvider[p.Provider] = p
	}
	assert.True(t, byProvider["openai"].Found)
	assert.Equal(t, "OPENAI_API_KEY", byProvider["openai"].EnvVar)
	assert.Equal(t, "environment", byProvider["openai"].Source)
	assert.False(t, byProvider["anthropic"].Found)
}

func writeDoctorAgentFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
agents:
  root:
    model: openai/gpt-5
    instruction: test agent
`), 0o600))
	return path
}

func TestDoctorCommand_AgentFileMissingVars(t *testing.T) {
	t.Parallel()

	path := writeDoctorAgentFile(t)

	output, err := executeDoctor(t, []string{path}, withDoctorTestEnv(
		map[string]string{"ANTHROPIC_API_KEY": "key"},
		[]string{"ai/qwen3:latest"}, nil))

	require.Error(t, err)
	statusErr, ok := errors.AsType[cli.StatusError](err)
	require.True(t, ok, "expected a cli.StatusError, got %T", err)
	assert.Equal(t, 1, statusErr.StatusCode)

	assert.Contains(t, output, "Agent requirements: "+path)
	assert.Regexp(t, `OPENAI_API_KEY\s+models\s+not set\s+-`, output)
	assert.Contains(t, output, "requires environment variables that are not set: OPENAI_API_KEY")
}

func TestDoctorCommand_AgentFileVarsSatisfied(t *testing.T) {
	t.Parallel()

	path := writeDoctorAgentFile(t)

	output, err := executeDoctor(t, []string{path}, withDoctorTestEnv(
		map[string]string{"OPENAI_API_KEY": "key"},
		[]string{"ai/qwen3:latest"}, nil))

	require.NoError(t, err)
	assert.Regexp(t, `OPENAI_API_KEY\s+models\s+found\s+environment`, output)
	assert.Contains(t, output, "No issues found.")
}

func TestDoctorCommand_AgentFileBehindGateway(t *testing.T) {
	t.Parallel()

	// A models gateway supplies model credentials, so the file's model env
	// vars are not required (mirrors the run-time preflight).
	path := writeDoctorAgentFile(t)

	output, err := executeDoctor(t, []string{"--models-gateway", "https://gateway.example.com", path},
		withDoctorTestEnv(nil, []string{"ai/qwen3:latest"}, nil))

	require.NoError(t, err)
	assert.Contains(t, output, "No environment variables required.")
	assert.Contains(t, output, "No issues found.")
}

func TestDoctorCommand_AgentFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := executeDoctor(t, []string{filepath.Join(t.TempDir(), "missing.yaml")},
		withDoctorTestEnv(nil, []string{"ai/qwen3:latest"}, nil))

	require.Error(t, err)
}

func TestCredentialCandidates(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "-", credentialCandidates(nil))
	assert.Equal(t, "OPENAI_API_KEY", credentialCandidates([]string{"OPENAI_API_KEY"}))
	assert.Equal(t, "GOOGLE_API_KEY (+2 more)", credentialCandidates([]string{"GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI"}))
}

func TestFindSource_SkipsSourcesWithoutValue(t *testing.T) {
	t.Parallel()

	sources := []environment.Source{
		{Name: "empty", Provider: environment.NewMapEnvProvider(map[string]string{"KEY": ""})},
		{Name: "none", Provider: environment.NewNoEnvProvider()},
		{Name: "filled", Provider: environment.NewMapEnvProvider(map[string]string{"KEY": "value"})},
	}

	source, found := findSource(t.Context(), sources, "KEY")
	require.True(t, found)
	assert.Equal(t, "filled", source)

	_, found = findSource(t.Context(), sources, "OTHER")
	assert.False(t, found)
}
