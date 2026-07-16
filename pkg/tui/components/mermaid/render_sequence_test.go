package mermaid

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderSequenceDiagramIncludesNotesAndAllMessages(t *testing.T) {
	t.Parallel()

	source := `sequenceDiagram
participant C as Client
participant S as Server
Note over C,S: TCP three-way handshake
C->>S: SYN, Seq = x
Note right of S: SYN received
S->>C: SYN-ACK, Seq = y, Ack = x + 1
Note left of C: Server acknowledges client's SYN
C->>S: ACK, Seq = x + 1, Ack = y + 1
Note over C,S: Connection established
C->>S: Data, Seq = x + 1
S->>C: ACK, Ack = next expected byte`

	rendered, ok := Render(source, 100)
	require.True(t, ok)
	for _, text := range []string{
		"TCP three-way handshake",
		"SYN, Seq = x",
		"SYN received",
		"SYN-ACK, Seq = y, Ack = x + 1",
		"Server acknowledges client's SYN",
		"ACK, Seq = x + 1, Ack = y + 1",
		"Connection established",
		"Data, Seq = x + 1",
		"ACK, Ack = next expected byte",
	} {
		assert.Contains(t, rendered, text)
	}
}
