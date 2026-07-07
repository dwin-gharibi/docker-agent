package root

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/telemetry"
)

// loginProviders lists the model providers that authenticate through a
// browser sign-in instead of an API key. Today only ChatGPT; the command
// shape (auth login <provider>) leaves room for more.
var loginProviders = []string{chatgpt.ProviderName}

// authSeams are the test seams for the auth commands: production wiring
// talks to the browser and the on-disk credential store, tests inject fakes.
type authSeams struct {
	login  func(ctx context.Context, out io.Writer) (*chatgpt.LoginResult, error)
	logout func() (bool, error)
	load   func() (*chatgpt.Credentials, error)
	isTTY  func() bool
}

type authCmdOption func(*authSeams)

func newAuthCmd(opts ...authCmdOption) *cobra.Command {
	seams := authSeams{
		login:  chatgpt.Login,
		logout: chatgpt.Logout,
		load:   chatgpt.Load,
		isTTY:  func() bool { return isatty.IsTerminal(os.Stdout.Fd()) },
	}
	for _, opt := range opts {
		opt(&seams)
	}

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Sign in to model providers that use an account instead of an API key",
		Long: `Manage account-based model provider sign-ins.

Some providers authenticate with an account subscription instead of an API
key. Signing in stores an OAuth credential that docker-agent refreshes
automatically; no environment variable is needed.

Supported providers:
  - chatgpt: use a ChatGPT Plus/Pro/Business subscription for chatgpt/* models`,
		Example: `  docker-agent auth login chatgpt
  docker-agent auth status
  docker-agent auth logout chatgpt`,
		GroupID: "core",
	}

	cmd.AddCommand(newAuthLoginCmd(&seams))
	cmd.AddCommand(newAuthLogoutCmd(&seams))
	cmd.AddCommand(newAuthStatusCmd(&seams))

	return cmd
}

// resolveLoginProvider validates the optional provider argument, defaulting
// to chatgpt (the only login-based provider today).
func resolveLoginProvider(args []string) (string, error) {
	if len(args) == 0 {
		return chatgpt.ProviderName, nil
	}
	for _, p := range loginProviders {
		if args[0] == p {
			return p, nil
		}
	}
	return "", fmt.Errorf("unknown provider %q; providers with account sign-in: %v", args[0], loginProviders)
}

func completeLoginProviders(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return loginProviders, cobra.ShellCompDirectiveNoFileComp
}

func newAuthLoginCmd(seams *authSeams) *cobra.Command {
	return &cobra.Command{
		Use:   "login [provider]",
		Short: "Sign in with a provider account (default: chatgpt)",
		Long: `Sign in with a provider account through the browser.

For chatgpt, this opens the ChatGPT sign-in page and stores the resulting
credential in the docker-agent config directory. The access token is
refreshed automatically; check the result with 'docker agent auth status'
or 'docker agent doctor'.`,
		Example:           `  docker-agent auth login chatgpt`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeLoginProviders,
		RunE: func(cmd *cobra.Command, args []string) (commandErr error) {
			ctx := cmd.Context()
			telemetry.TrackCommand(ctx, "auth", append([]string{"login"}, args...))
			defer func() { // do not inline this defer so that commandErr is not resolved early
				telemetry.TrackCommandError(ctx, "auth", append([]string{"login"}, args...), commandErr)
			}()

			provider, err := resolveLoginProvider(args)
			if err != nil {
				return err
			}

			if !seams.isTTY() {
				return fmt.Errorf("`docker agent auth login %s` needs a browser and a terminal\n"+
					"Without one, sign in on a workstation and set %s explicitly", provider, chatgpt.TokenEnvVar)
			}

			w := cmd.OutOrStdout()
			result, err := seams.login(ctx, w)
			if err != nil {
				return err
			}

			printLoginSuccess(w, result)
			return nil
		},
	}
}

func printLoginSuccess(w io.Writer, result *chatgpt.LoginResult) {
	fmt.Fprintln(w, "Signed in with your ChatGPT account.")
	if result.Email != "" {
		fmt.Fprintf(w, "  Account: %s\n", result.Email)
	}
	if result.Plan != "" {
		fmt.Fprintf(w, "  Plan:    %s\n", result.Plan)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Start chatting with:\n\n  docker agent run --model %s/%s\n\n", chatgpt.ProviderName, config.DefaultModels[chatgpt.ProviderName])
	fmt.Fprintln(w, "Check your setup anytime with `docker agent doctor`.")
}

func newAuthLogoutCmd(seams *authSeams) *cobra.Command {
	return &cobra.Command{
		Use:               "logout [provider]",
		Short:             "Remove a stored provider sign-in (default: chatgpt)",
		Example:           `  docker-agent auth logout chatgpt`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeLoginProviders,
		RunE: func(cmd *cobra.Command, args []string) (commandErr error) {
			ctx := cmd.Context()
			telemetry.TrackCommand(ctx, "auth", append([]string{"logout"}, args...))
			defer func() { // do not inline this defer so that commandErr is not resolved early
				telemetry.TrackCommandError(ctx, "auth", append([]string{"logout"}, args...), commandErr)
			}()

			provider, err := resolveLoginProvider(args)
			if err != nil {
				return err
			}

			removed, err := seams.logout()
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s.\n", provider)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "No %s sign-in was stored.\n", provider)
			}
			return nil
		},
	}
}

// authStatusEntry is the machine-readable sign-in state for one provider.
// Token values are never included.
type authStatusEntry struct {
	Provider  string    `json:"provider"`
	SignedIn  bool      `json:"signed_in"`
	Email     string    `json:"email,omitempty"`
	Plan      string    `json:"plan,omitempty"`
	AccountID string    `json:"account_id,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	Expired   bool      `json:"expired,omitempty"`
	Refresh   bool      `json:"has_refresh_token,omitempty"`
}

func newAuthStatusCmd(seams *authSeams) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Show stored provider sign-ins",
		Example: `  docker-agent auth status`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (commandErr error) {
			ctx := cmd.Context()
			telemetry.TrackCommand(ctx, "auth", []string{"status"})
			defer func() { // do not inline this defer so that commandErr is not resolved early
				telemetry.TrackCommandError(ctx, "auth", []string{"status"}, commandErr)
			}()

			entry := authStatusEntry{Provider: chatgpt.ProviderName}
			if creds, err := seams.load(); err == nil {
				entry.SignedIn = true
				entry.Email = creds.Email
				entry.Plan = creds.Plan
				entry.AccountID = creds.AccountID
				entry.ExpiresAt = creds.ExpiresAt
				entry.Expired = creds.Expired()
				entry.Refresh = creds.RefreshToken != ""
			}

			w := cmd.OutOrStdout()
			if jsonOutput {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode([]authStatusEntry{entry})
			}

			printAuthStatusText(w, entry)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func printAuthStatusText(w io.Writer, entry authStatusEntry) {
	if !entry.SignedIn {
		fmt.Fprintf(w, "%s: not signed in (run `docker agent auth login %s`)\n", entry.Provider, entry.Provider)
		return
	}

	fmt.Fprintf(w, "%s: signed in\n", entry.Provider)
	if entry.Email != "" {
		fmt.Fprintf(w, "  Account:    %s\n", entry.Email)
	}
	if entry.Plan != "" {
		fmt.Fprintf(w, "  Plan:       %s\n", entry.Plan)
	}
	if !entry.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "  Expires at: %s\n", entry.ExpiresAt.Local().Format(time.RFC3339))
	}
	if entry.Expired {
		if entry.Refresh {
			fmt.Fprintln(w, "  Status:     access token expired (refreshed automatically on next use)")
		} else {
			fmt.Fprintf(w, "  Status:     expired; sign in again with `docker agent auth login %s`\n", entry.Provider)
		}
	}
}
