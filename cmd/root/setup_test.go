package root

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider/dmr"
)

// fakeSecretStore records stored secrets in memory and can be made to fail.
type fakeSecretStore struct {
	name   string
	stored map[string]string
	err    error
}

func (s *fakeSecretStore) Name() string        { return s.name }
func (s *fakeSecretStore) Description() string { return s.name + " (fake)" }

func (s *fakeSecretStore) Store(_ context.Context, name, value string) error {
	if s.err != nil {
		return s.err
	}
	if s.stored == nil {
		s.stored = map[string]string{}
	}
	s.stored[name] = value
	return nil
}

// newTestWizard builds a wizard fed by scripted answers, returning the output
// buffer. Secrets are answered by keys (one per prompt); DMR state comes from
// dmrModels/dmrErr.
func newTestWizard(answers string, keys []string, stores []environment.SecretStore, dmrModels []string, dmrErr error) (*setupWizard, *bytes.Buffer, *[]string) {
	var out bytes.Buffer
	var pulled []string
	secretCalls := 0

	wizard := &setupWizard{
		in:  bufio.NewReader(strings.NewReader(answers)),
		out: &out,
		readSecret: func(string) (string, error) {
			if secretCalls >= len(keys) {
				return "", errors.New("no scripted key left")
			}
			key := keys[secretCalls]
			secretCalls++
			return key, nil
		},
		stores:    stores,
		dmrLister: func(context.Context) ([]string, error) { return dmrModels, dmrErr },
		pullModel: func(_ context.Context, model string) error {
			pulled = append(pulled, model)
			return nil
		},
	}
	return wizard, &out, &pulled
}

func TestSetupWizard_CloudPathStoresKey(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{name: "keychain"}
	// cloud -> provider 1 (anthropic) -> store 1
	wizard, out, _ := newTestWizard("1\n1\n1\n", []string{"sk-cloud-key"}, []environment.SecretStore{store}, nil, nil)

	result, err := wizard.run(t.Context())
	require.NoError(t, err)

	assert.Equal(t, "sk-cloud-key", store.stored["ANTHROPIC_API_KEY"])
	assert.Equal(t, "ANTHROPIC_API_KEY", result.EnvVar)
	assert.Equal(t, "sk-cloud-key", result.Value)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", result.Model)

	output := out.String()
	assert.Contains(t, output, "Stored ANTHROPIC_API_KEY in the keychain (fake).")
	assert.Contains(t, output, "docker agent run")
	assert.Contains(t, output, "--model anthropic/claude-sonnet-4-6")
	assert.Contains(t, output, "docker agent doctor")
	assert.NotContains(t, output, "sk-cloud-key", "secret values must never be printed")
}

func TestSetupWizard_DefaultsSelectCloudAndFirstEntries(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{name: "keychain"}
	// Empty answers take every default: cloud, first provider, first store.
	wizard, _, _ := newTestWizard("\n\n\n", []string{"sk-key"}, []environment.SecretStore{store}, nil, nil)

	result, err := wizard.run(t.Context())
	require.NoError(t, err)

	first := config.CloudProviderEnvVars()[0]
	assert.Equal(t, first.EnvVars[0], result.EnvVar)
	assert.Equal(t, "sk-key", store.stored[first.EnvVars[0]])
}

func TestSetupWizard_CloudPathRetriesFailedStore(t *testing.T) {
	t.Parallel()

	broken := &fakeSecretStore{name: "pass", err: errors.New("password store is empty")}
	working := &fakeSecretStore{name: "config-env-file"}
	// cloud -> provider 1 -> store 1 (fails) -> store 2 (succeeds)
	wizard, out, _ := newTestWizard("1\n1\n1\n2\n", []string{"sk-key"},
		[]environment.SecretStore{broken, working}, nil, nil)

	result, err := wizard.run(t.Context())
	require.NoError(t, err)

	assert.Contains(t, out.String(), "Could not store the key: password store is empty")
	assert.Equal(t, "sk-key", working.stored[result.EnvVar])
}

func TestSetupWizard_CloudPathReasksOnEmptyKey(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{name: "keychain"}
	wizard, out, _ := newTestWizard("1\n1\n1\n", []string{"  ", "sk-key"}, []environment.SecretStore{store}, nil, nil)

	_, err := wizard.run(t.Context())
	require.NoError(t, err)

	assert.Contains(t, out.String(), "The key is empty")
	assert.Equal(t, "sk-key", store.stored["ANTHROPIC_API_KEY"])
}

func TestSetupWizard_LocalPathWithPulledModels(t *testing.T) {
	t.Parallel()

	wizard, out, pulled := newTestWizard("2\n", nil, nil, []string{"ai/smollm2"}, nil)

	result, err := wizard.run(t.Context())
	require.NoError(t, err)

	assert.Empty(t, *pulled, "no pull needed when a model is already available")
	assert.Equal(t, "dmr/ai/smollm2", result.Model)
	assert.Contains(t, out.String(), "- ai/smollm2")
	assert.Contains(t, out.String(), "docker agent run")
}

func TestSetupWizard_LocalPathPullsDefaultModel(t *testing.T) {
	t.Parallel()

	// local -> accept the default model
	wizard, out, pulled := newTestWizard("2\n\n", nil, nil, nil, nil)

	result, err := wizard.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, []string{"ai/qwen3:latest"}, *pulled)
	assert.Equal(t, "dmr/ai/qwen3:latest", result.Model)
	assert.Contains(t, out.String(), "no model is pulled yet")
}

func TestSetupWizard_LocalPathPullsCustomModel(t *testing.T) {
	t.Parallel()

	wizard, _, pulled := newTestWizard("2\nai/llama3.2\n", nil, nil, nil, nil)

	result, err := wizard.run(t.Context())
	require.NoError(t, err)

	require.Equal(t, []string{"ai/llama3.2"}, *pulled)
	assert.Equal(t, "dmr/ai/llama3.2", result.Model)
}

func TestSetupWizard_LocalPathDMRNotInstalled(t *testing.T) {
	t.Parallel()

	wizard, _, _ := newTestWizard("2\n", nil, nil, nil, dmr.ErrNotInstalled)

	_, err := wizard.run(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
	assert.Contains(t, err.Error(), dmrDocsURL)
}

func TestSetupWizard_LocalPathDMRUnreachable(t *testing.T) {
	t.Parallel()

	wizard, _, _ := newTestWizard("2\n", nil, nil, nil, errors.New("connection refused"))

	_, err := wizard.run(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not reachable")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestSetupWizard_InvalidChoiceReasks(t *testing.T) {
	t.Parallel()

	wizard, out, pulled := newTestWizard("9\nx\n2\n", nil, nil, []string{"ai/smollm2"}, nil)

	_, err := wizard.run(t.Context())
	require.NoError(t, err)

	assert.Contains(t, out.String(), "Enter a number between 1 and 2.")
	assert.Empty(t, *pulled)
}

func TestSetupWizard_EOFCancels(t *testing.T) {
	t.Parallel()

	wizard, _, _ := newTestWizard("", nil, nil, nil, nil)

	_, err := wizard.run(t.Context())
	require.ErrorIs(t, err, errSetupCancelled)
}

func TestErrorIndicatesNoUsableModel(t *testing.T) {
	t.Parallel()

	assert.True(t, errorIndicatesNoUsableModel(&config.AutoModelFallbackError{}))
	assert.True(t, errorIndicatesNoUsableModel(fmt.Errorf("loading team: %w", dmr.ErrNotInstalled)))
	assert.True(t, errorIndicatesNoUsableModel(&environment.RequiredEnvError{
		Missing: []string{"OPENAI_API_KEY"}, MissingModelCredentials: true,
	}))

	// Missing tool secrets are not fixable by the model setup wizard.
	assert.False(t, errorIndicatesNoUsableModel(&environment.RequiredEnvError{
		Missing: []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
	}))
	assert.False(t, errorIndicatesNoUsableModel(errors.New("boom")))
	assert.False(t, errorIndicatesNoUsableModel(nil))
	// Pull errors already carry their own exact remediation.
	assert.False(t, errorIndicatesNoUsableModel(&dmr.PullFailedError{Model: "ai/qwen3"}))
}

func TestErrorIndicatesNoUsableModel_FirstAvailableVariant(t *testing.T) {
	t.Parallel()

	cfg := &latest.Config{
		Models: map[string]latest.ModelConfig{
			"smart": {FirstAvailable: []string{"openai/gpt-5"}},
		},
	}
	err := config.ResolveFirstAvailableModels(t.Context(), cfg, "", environment.NewNoEnvProvider())
	require.Error(t, err)

	assert.True(t, errorIndicatesNoUsableModel(err))
}

func TestSetupOfferDisabledByEnv(t *testing.T) {
	t.Parallel()

	assert.False(t, setupOfferDisabledByEnv(func(string) string { return "" }))
	for _, name := range []string{"DOCKER_AGENT_NO_SETUP", "CAGENT_NO_SETUP"} {
		env := func(key string) string {
			if key == name {
				return "1"
			}
			return ""
		}
		assert.True(t, setupOfferDisabledByEnv(env), name)
	}
}

func TestShouldOfferSetup(t *testing.T) {
	t.Parallel()

	noEnv := func(string) string { return "" }
	modelErr := &config.AutoModelFallbackError{}

	assert.False(t, shouldOfferSetup(nil, false, noEnv))
	assert.False(t, shouldOfferSetup(modelErr, true, noEnv), "exec mode never prompts")
	assert.False(t, shouldOfferSetup(modelErr, false, func(string) string { return "1" }))
	// The remaining conditions require a real terminal on stdin and stdout,
	// which a test process does not have: the guard must say no here.
	assert.False(t, shouldOfferSetup(modelErr, false, noEnv))
}

func TestSetupCommand_RequiresTerminal(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newSetupCmd()
	cmd.SilenceErrors = true
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a terminal")
	assert.Contains(t, err.Error(), environment.SecretsDocsURL)
}
