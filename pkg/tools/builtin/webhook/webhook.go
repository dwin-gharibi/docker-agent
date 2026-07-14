package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/httpclient"
	"github.com/docker/docker-agent/pkg/tools"
)

const (
	ToolNameSendWebhook = "send_webhook"

	category       = "webhook"
	requestTimeout = 30 * time.Second
	maxRespRead    = 64 << 10
)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type ToolSet struct {
	client httpDoer
}

var (
	_ tools.ToolSet      = (*ToolSet)(nil)
	_ tools.Instructable = (*ToolSet)(nil)
)

func New() *ToolSet {
	return &ToolSet{client: httpclient.NewSafeClient(requestTimeout, false)}
}

func CreateToolSet(_ *config.RuntimeConfig) (tools.ToolSet, error) {
	return New(), nil
}

var supportedProviders = []string{
	"slack", "discord", "ifttt", "telegram",
	"mattermost", "rocketchat", "googlechat", "teams", "generic",
}

type SendArgs struct {
	URL      string `json:"url" jsonschema:"The webhook URL to POST to"`
	Message  string `json:"message" jsonschema:"The message text to send"`
	Provider string `json:"provider,omitempty" jsonschema:"Payload format: slack, discord, ifttt, telegram, mattermost, rocketchat, googlechat, teams, or generic (default generic)"`
	Value2   string `json:"value2,omitempty" jsonschema:"IFTTT value2 field (provider=ifttt only)"`
	Value3   string `json:"value3,omitempty" jsonschema:"IFTTT value3 field (provider=ifttt only)"`
	ChatID   string `json:"chat_id,omitempty" jsonschema:"Telegram chat id (provider=telegram only)"`
}

func normalizeProvider(p string) string {
	switch p = strings.ToLower(strings.TrimSpace(p)); p {
	case "":
		return "generic"
	case "google_chat", "gchat":
		return "googlechat"
	case "msteams", "microsoftteams", "microsoft_teams":
		return "teams"
	case "rocket.chat", "rocket_chat":
		return "rocketchat"
	default:
		return p
	}
}

func buildPayload(provider, message, value2, value3, chatID string) (string, []byte, error) {
	var payload map[string]string
	switch normalizeProvider(provider) {
	case "generic", "slack", "mattermost", "rocketchat", "googlechat", "teams":
		payload = map[string]string{"text": message}
	case "discord":
		payload = map[string]string{"content": message}
	case "ifttt":
		payload = map[string]string{"value1": message}
		if value2 != "" {
			payload["value2"] = value2
		}
		if value3 != "" {
			payload["value3"] = value3
		}
	case "telegram":
		if strings.TrimSpace(chatID) == "" {
			return "", nil, fmt.Errorf("telegram requires chat_id")
		}
		payload = map[string]string{"chat_id": chatID, "text": message}
	default:
		return "", nil, fmt.Errorf("unknown provider %q (supported: %s)", provider, strings.Join(supportedProviders, ", "))
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	return "application/json", body, nil
}

func (t *ToolSet) send(ctx context.Context, args SendArgs) (*tools.ToolCallResult, error) {
	if strings.TrimSpace(args.URL) == "" {
		return tools.ResultError("Error: url is required."), nil
	}
	if strings.TrimSpace(args.Message) == "" {
		return tools.ResultError("Error: message is required."), nil
	}
	if u, err := url.Parse(args.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return tools.ResultError("Error: url must be a valid http(s) URL."), nil
	}

	contentType, body, err := buildPayload(args.Provider, args.Message, args.Value2, args.Value3, args.ChatID)
	if err != nil {
		return tools.ResultError("Error: " + err.Error()), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, args.URL, bytes.NewReader(body))
	if err != nil {
		return tools.ResultError("Error: " + err.Error()), nil
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ResultError("Error: sending webhook: " + err.Error()), nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxRespRead))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.ResultError(fmt.Sprintf("Webhook returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))), nil
	}
	return tools.ResultSuccess(fmt.Sprintf("Delivered to %s webhook (HTTP %d).", normalizeProvider(args.Provider), resp.StatusCode)), nil
}

func (t *ToolSet) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:                    ToolNameSendWebhook,
			Category:                category,
			Description:             "Send a message to a webhook (Slack, Discord, IFTTT, or a generic URL). POSTs a provider-shaped JSON payload and reports delivery status.",
			Parameters:              tools.MustSchemaFor[SendArgs](),
			OutputSchema:            tools.MustSchemaFor[string](),
			Handler:                 tools.NewHandler(t.send),
			Annotations:             tools.ToolAnnotations{Title: "Send Webhook"},
			AddDescriptionParameter: true,
		},
	}, nil
}

func (t *ToolSet) Instructions() string {
	return `## Webhook Tool

Send an outbound notification with send_webhook(url, message, provider?):

- provider shapes the payload — slack, discord, ifttt, telegram, mattermost,
  rocketchat, googlechat, teams, or generic (default). Telegram needs chat_id.
- Delivery is one-way; the tool reports the HTTP status, not a response body.
- Requests to non-public addresses are refused.`
}
