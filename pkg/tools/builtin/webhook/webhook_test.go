package webhook

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeDoer struct {
	gotReq  *http.Request
	gotBody []byte
	status  int
	err     error
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.gotReq = req
	if req.Body != nil {
		f.gotBody, _ = io.ReadAll(req.Body)
	}
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}, nil
}

func newTestToolSet(d httpDoer) *ToolSet {
	ts := New()
	ts.client = d
	return ts
}

func decode(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var m map[string]string
	require.NoError(t, json.Unmarshal(body, &m))
	return m
}

func TestBuildPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		wantKey  string
	}{
		{"slack", "text"},
		{"discord", "content"},
		{"mattermost", "text"},
		{"rocketchat", "text"},
		{"googlechat", "text"},
		{"teams", "text"},
		{"msteams", "text"},
		{"generic", "text"},
		{"", "text"},
	}
	for _, tc := range tests {
		ct, body, err := buildPayload(tc.provider, "hello", "", "", "")
		require.NoError(t, err, tc.provider)
		require.Equal(t, "application/json", ct)
		require.Equal(t, "hello", decode(t, body)[tc.wantKey], tc.provider)
	}
}

func TestBuildPayloadIFTTT(t *testing.T) {
	t.Parallel()

	_, body, err := buildPayload("ifttt", "msg", "two", "three", "")
	require.NoError(t, err)
	m := decode(t, body)
	require.Equal(t, "msg", m["value1"])
	require.Equal(t, "two", m["value2"])
	require.Equal(t, "three", m["value3"])
}

func TestBuildPayloadTelegram(t *testing.T) {
	t.Parallel()

	_, body, err := buildPayload("telegram", "hi", "", "", "12345")
	require.NoError(t, err)
	m := decode(t, body)
	require.Equal(t, "12345", m["chat_id"])
	require.Equal(t, "hi", m["text"])

	_, _, err = buildPayload("telegram", "hi", "", "", "")
	require.Error(t, err)
}

func TestBuildPayloadUnknownProvider(t *testing.T) {
	t.Parallel()

	_, _, err := buildPayload("webex", "x", "", "", "")
	require.Error(t, err)
}

func TestSendSuccess(t *testing.T) {
	t.Parallel()

	fd := &fakeDoer{status: 200}
	ts := newTestToolSet(fd)

	res, err := ts.send(t.Context(), SendArgs{
		URL: "https://hooks.slack.com/services/xxx", Message: "deploy done", Provider: "slack",
	})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Output)

	require.Equal(t, http.MethodPost, fd.gotReq.Method)
	require.Equal(t, "application/json", fd.gotReq.Header.Get("Content-Type"))
	require.Equal(t, "deploy done", decode(t, fd.gotBody)["text"])
}

func TestSendNon2xx(t *testing.T) {
	t.Parallel()

	ts := newTestToolSet(&fakeDoer{status: 404})
	res, err := ts.send(t.Context(), SendArgs{URL: "https://example.com/hook", Message: "hi"})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "404")
}

func TestSendNetworkError(t *testing.T) {
	t.Parallel()

	ts := newTestToolSet(&fakeDoer{err: io.ErrUnexpectedEOF})
	res, err := ts.send(t.Context(), SendArgs{URL: "https://example.com/hook", Message: "hi"})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestSendValidation(t *testing.T) {
	t.Parallel()

	ts := newTestToolSet(&fakeDoer{})

	r1, _ := ts.send(t.Context(), SendArgs{Message: "hi"})
	require.True(t, r1.IsError)

	r2, _ := ts.send(t.Context(), SendArgs{URL: "https://x/y"})
	require.True(t, r2.IsError)

	r3, _ := ts.send(t.Context(), SendArgs{URL: "ftp://x/y", Message: "hi"})
	require.True(t, r3.IsError)

	r4, _ := ts.send(t.Context(), SendArgs{URL: "https://x/y", Message: "hi", Provider: "webex"})
	require.True(t, r4.IsError)
}

func TestToolSetInterfaces(t *testing.T) {
	t.Parallel()

	ts := New()
	require.NotEmpty(t, ts.Instructions())
	toolz, err := ts.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolz, 1)
	require.Equal(t, ToolNameSendWebhook, toolz[0].Name)
}
