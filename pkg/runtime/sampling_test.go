package runtime

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
)

func TestSamplingMessagesToChat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     *mcp.CreateMessageParams
		want    []chat.Message
		wantErr bool
	}{
		{
			name: "single user text message",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Role: "user", Content: &mcp.TextContent{Text: "hello"}},
				},
			},
			want: []chat.Message{
				{Role: chat.MessageRoleUser, Content: "hello"},
			},
		},
		{
			name: "system prompt is prepended",
			req: &mcp.CreateMessageParams{
				SystemPrompt: "be terse",
				Messages: []*mcp.SamplingMessage{
					{Role: "user", Content: &mcp.TextContent{Text: "hi"}},
				},
			},
			want: []chat.Message{
				{Role: chat.MessageRoleSystem, Content: "be terse"},
				{Role: chat.MessageRoleUser, Content: "hi"},
			},
		},
		{
			name: "user and assistant turns are preserved",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Role: "user", Content: &mcp.TextContent{Text: "ping"}},
					{Role: "assistant", Content: &mcp.TextContent{Text: "pong"}},
					{Role: "user", Content: &mcp.TextContent{Text: "again"}},
				},
			},
			want: []chat.Message{
				{Role: chat.MessageRoleUser, Content: "ping"},
				{Role: chat.MessageRoleAssistant, Content: "pong"},
				{Role: chat.MessageRoleUser, Content: "again"},
			},
		},
		{
			name: "image content becomes a data URL multi-part",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{
						Role:    "user",
						Content: &mcp.ImageContent{Data: []byte("PNGBYTES"), MIMEType: "image/png"},
					},
				},
			},
			want: []chat.Message{
				{
					Role: chat.MessageRoleUser,
					MultiContent: []chat.MessagePart{{
						Type: chat.MessagePartTypeImageURL,
						ImageURL: &chat.MessageImageURL{
							URL: "data:image/png;base64,UE5HQllURVM=",
						},
					}},
				},
			},
		},
		{
			name: "audio content falls back to a text placeholder",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Role: "user", Content: &mcp.AudioContent{Data: []byte("WAV"), MIMEType: "audio/wav"}},
				},
			},
			want: []chat.Message{
				{Role: chat.MessageRoleUser, Content: "[audio attachment (audio/wav, 3 bytes) — not inlined]"},
			},
		},
		{
			name: "missing role defaults to user",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Content: &mcp.TextContent{Text: "anonymous"}},
				},
			},
			want: []chat.Message{
				{Role: chat.MessageRoleUser, Content: "anonymous"},
			},
		},
		{
			name: "unsupported role surfaces as an error",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Role: "tool", Content: &mcp.TextContent{Text: "nope"}},
				},
			},
			wantErr: true,
		},
		{
			name:    "empty request is rejected",
			req:     &mcp.CreateMessageParams{},
			wantErr: true,
		},
		{
			name: "system-prompt-only request is rejected",
			req: &mcp.CreateMessageParams{
				SystemPrompt: "no messages, only a system prompt",
			},
			wantErr: true,
		},
		{
			name: "nil message entry is rejected",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{nil},
			},
			wantErr: true,
		},
		{
			name: "oversize text block is rejected",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Role: "user", Content: &mcp.TextContent{Text: strings.Repeat("a", maxSamplingTextBytes+1)}},
				},
			},
			wantErr: true,
		},
		{
			name: "oversize image block is rejected",
			req: &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Role: "user", Content: &mcp.ImageContent{Data: make([]byte, maxSamplingBinaryBytes+1), MIMEType: "image/png"}},
				},
			},
			wantErr: true,
		},
		{
			name: "oversize system prompt is rejected",
			req: &mcp.CreateMessageParams{
				SystemPrompt: strings.Repeat("a", maxSamplingTextBytes+1),
				Messages: []*mcp.SamplingMessage{
					{Role: "user", Content: &mcp.TextContent{Text: "hi"}},
				},
			},
			wantErr: true,
		},
		{
			name: "too many messages is rejected",
			req: &mcp.CreateMessageParams{
				Messages: tooManyMessages(maxSamplingMessages + 1),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := samplingMessagesToChat(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func tooManyMessages(n int) []*mcp.SamplingMessage {
	out := make([]*mcp.SamplingMessage, n)
	for i := range out {
		out[i] = &mcp.SamplingMessage{Role: "user", Content: &mcp.TextContent{Text: "x"}}
	}
	return out
}

func TestStopReasonMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   chat.FinishReason
		want string
	}{
		{chat.FinishReasonStop, "endTurn"},
		{chat.FinishReasonLength, "maxTokens"},
		{chat.FinishReasonToolCalls, "toolUse"},
		{chat.FinishReasonNull, "endTurn"},
		{"", "endTurn"},
	}

	for _, tt := range tests {
		t.Run(string(tt.in), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, stopReason(tt.in))
		})
	}
}

func TestDataURL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "data:image/png;base64,UE5HQllURVM=", dataURL("image/png", []byte("PNGBYTES")))
	assert.Equal(t, "data:application/octet-stream;base64,YQ==", dataURL("", []byte("a")))
}
