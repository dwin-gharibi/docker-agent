package environment

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/docker-agent/pkg/atomicfile"
	"github.com/docker/docker-agent/pkg/paths"
)

// SecretStore persists a secret so that one of the default secret sources
// ([DefaultSources]) can resolve it on later runs. Implementations are the
// write-side counterparts of the read-side providers: a value stored under a
// name must be returned by the matching provider's Get for that same name.
type SecretStore interface {
	// Name identifies the matching read-side source (e.g. "keychain").
	Name() string
	// Description is a short human-readable label for interactive pickers.
	Description() string
	// Store persists value under name, replacing any previous value.
	Store(ctx context.Context, name, value string) error
}

// SecretStores returns the secret stores usable on this machine, most
// preferred first: the OS-native store when its tooling is available
// (macOS Keychain, pass elsewhere), then the docker agent env file, which is
// always available as a plain-file fallback.
func SecretStores() []SecretStore {
	var stores []SecretStore

	if runtime.GOOS == "darwin" {
		if keychain, err := NewKeychainStore(); err == nil {
			stores = append(stores, keychain)
		}
	}
	if pass, err := NewPassStore(); err == nil {
		stores = append(stores, pass)
	}

	return append(stores, NewConfigEnvFileStore())
}

// KeychainStore stores secrets as generic passwords in the macOS keychain,
// where [KeychainProvider] resolves them.
type KeychainStore struct {
	binaryPath string
}

// NewKeychainStore creates a KeychainStore. It verifies that the `security`
// command is available and stores the resolved absolute path.
func NewKeychainStore() (*KeychainStore, error) {
	path, err := lookupBinary("security", KeychainNotAvailableError{})
	if err != nil {
		return nil, err
	}
	return &KeychainStore{binaryPath: path}, nil
}

func (s *KeychainStore) Name() string { return "keychain" }

func (s *KeychainStore) Description() string { return "macOS Keychain" }

// Store writes the secret with `security -i` so the value travels over stdin
// and never appears in the process argument list. The fixed account name
// keeps -U updating the same item on re-runs (a different account would
// create a duplicate item instead); [KeychainProvider] looks up by service
// name only, so the account value never affects reads.
func (s *KeychainStore) Store(ctx context.Context, name, value string) error {
	command := fmt.Sprintf("add-generic-password -U -a %s -s %s -w %s\n",
		securityQuote(keychainAccountName()), securityQuote(name), securityQuote(value))

	cmd := exec.CommandContext(ctx, s.binaryPath, "-i")
	cmd.Stdin = strings.NewReader(command)
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("storing %s in the macOS Keychain: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func keychainAccountName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "docker-agent"
}

// securityQuote quotes a token for the `security -i` command parser, which
// splits on whitespace and honours double quotes with backslash escapes.
func securityQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// PassStore stores secrets in the `pass` password manager, where
// [PassProvider] resolves them.
type PassStore struct {
	binaryPath string
}

// NewPassStore creates a PassStore. It verifies that `pass` is available and
// stores the resolved absolute path.
func NewPassStore() (*PassStore, error) {
	path, err := lookupBinary("pass", PassNotAvailableError{})
	if err != nil {
		return nil, err
	}
	return &PassStore{binaryPath: path}, nil
}

func (s *PassStore) Name() string { return "pass" }

func (s *PassStore) Description() string { return "pass (password manager)" }

// Store inserts the secret with `pass insert -e -f`, feeding the value over
// stdin (echo mode reads a single line) so it never appears in the process
// argument list. -f replaces any existing entry without prompting.
func (s *PassStore) Store(ctx context.Context, name, value string) error {
	cmd := exec.CommandContext(ctx, s.binaryPath, "insert", "-e", "-f", name)
	cmd.Stdin = strings.NewReader(value + "\n")
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("storing %s in pass: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// EnvFileStore stores secrets as KEY=value lines in an env file. The file at
// [ConfigEnvFilePath] is read by the default source chain, so values stored
// there resolve on every later run without extra flags.
type EnvFileStore struct {
	path string
}

// NewConfigEnvFileStore creates an EnvFileStore writing to the docker agent
// config env file (see [ConfigEnvFilePath]).
func NewConfigEnvFileStore() *EnvFileStore {
	return &EnvFileStore{path: ConfigEnvFilePath()}
}

func (s *EnvFileStore) Name() string { return "config-env-file" }

func (s *EnvFileStore) Description() string {
	return fmt.Sprintf("docker agent env file (%s, plain text)", s.path)
}

// Store writes NAME=value to the env file, replacing the line for an existing
// NAME and preserving every other line (comments included). The file is
// created private (0600) in a private directory and written atomically.
func (s *EnvFileStore) Store(_ context.Context, name, value string) error {
	if name == "" || strings.ContainsAny(name, "=\n") {
		return fmt.Errorf("invalid environment variable name %q", name)
	}
	if strings.Contains(value, "\n") {
		return fmt.Errorf("value for %s cannot contain newlines", name)
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating config directory for %s: %w", s.path, err)
	}

	existing, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", s.path, err)
	}

	content := upsertEnvLine(string(existing), name, value)
	if err := atomicfile.Write(s.path, strings.NewReader(content), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	return nil
}

// upsertEnvLine replaces the NAME=... line in content or appends one,
// returning the new file content. Existing lines are preserved verbatim so
// user comments and unrelated variables survive updates.
func upsertEnvLine(content, name, value string) string {
	newLine := name + "=" + value

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	replaced := false
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == name {
			lines[i] = newLine
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, newLine)
	}

	return strings.Join(lines, "\n") + "\n"
}

// ConfigEnvFilePath returns the path of the env file that the default secret
// sources read: <config dir>/.env (e.g. ~/.config/cagent/.env).
func ConfigEnvFilePath() string {
	return filepath.Join(paths.GetConfigDir(), ".env")
}
