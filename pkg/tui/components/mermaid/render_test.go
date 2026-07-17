package mermaid

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"
)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		width  int
	}{
		{
			name:   "flowchart top down",
			source: "flowchart TD\nA[Start] -->|go| B[Finish]",
			width:  60,
		},
		{
			name:   "flowchart top bottom alias",
			source: "flowchart TB\nA[Start] -->|go| B[Finish]",
			width:  60,
		},
		{
			name:   "flowchart bottom top",
			source: "flowchart BT\nA[Start] -->|go| B[Finish]",
			width:  60,
		},
		{
			name:   "flowchart left right",
			source: "flowchart LR\nA[Start] -->|go| B[Finish]",
			width:  60,
		},
		{
			name:   "flowchart right left",
			source: "flowchart RL\nA[Start] -->|go| B[Finish]",
			width:  60,
		},
		{
			name:   "right left structural labels",
			source: "flowchart RL\nA[╭ A ▶ B ┤] -->|╰ edge ▶ ╮| B[Target]",
			width:  100,
		},
		{
			name:   "right left chain",
			source: "flowchart RL\nClient --> API\nAPI --> Database",
			width:  100,
		},
		{
			name: "right left branching",
			source: `flowchart RL
A[Start] --> B{Decision}
B -->|Yes| C[Option A]
B -->|No| D[Option B]
C --> E[Finish]
D --> E`,
			width: 100,
		},
		{
			name:   "horizontal two branches",
			source: "flowchart LR\nA[Root] --> B[Top]\nA --> C[Bottom]",
			width:  100,
		},
		{
			name:   "horizontal branching",
			source: "flowchart LR\nA[Root] --> B[Top]\nA --> C[Middle]\nA --> D[Bottom]",
			width:  100,
		},
		{
			name:   "narrow horizontal fallback",
			source: "flowchart LR\nA[Long starting node] --> B[Long finishing node]",
			width:  16,
		},
		{
			name: "subgraph",
			source: `flowchart LR
client[Client] --> api
subgraph backend[Backend]
  api[API] --> db[Database]
  api --> cache[Cache]
end`,
			width: 100,
		},
		{
			name: "subgraph whole node label",
			source: `flowchart LR
long[DeployProd] --> short[Deploy]
subgraph production[Production]
  short
end`,
			width: 80,
		},
		{
			name: "subgraph right padding",
			source: `flowchart LR
subgraph infrastructure[Infrastructure]
  runtime[Tool Runtime] --> docker[Docker]
end`,
			width: 80,
		},
		{
			name: "sibling subgraphs",
			source: `flowchart LR
subgraph agents[Agents]
  router[Router] --> developer[Developer]
end
subgraph infra[Infrastructure]
  runtime[Runtime] --> docker[Docker]
end
developer --> runtime`,
			width: 100,
		},
		{
			name: "nested subgraphs",
			source: `flowchart TD
subgraph platform[Platform]
  gateway[Gateway] --> api
  subgraph services[Services]
    api[API] --> db[Database]
  end
end`,
			width: 80,
		},
		{
			name: "flowchart node shapes and quoted labels",
			source: `flowchart LR
rect[Rectangle] --> rounded(Rounded)
rounded --> stadium([Stadium])
stadium --> subroutine[[Subroutine]]
subroutine --> database[(Database)]
database --> circle((Circle))
circle --> decision{Decision}
decision --> hexagon{{Hexagon}}
hexagon --> quoted["Quoted; label"]`,
			width: 160,
		},
		{
			name: "flowchart shared target and cycle",
			source: `flowchart TD
A[Start] --> B[Left]
A --> C[Right]
B --> D[Merge]
C --> D
D --> B`,
			width: 80,
		},
		{
			name: "flowchart standalone and disconnected components",
			source: `flowchart TD
A[First] --> B[Second]
Standalone[No connections]
C[Third] --> D[Fourth]`,
			width: 60,
		},
		{
			name: "flowchart unicode labels",
			source: `flowchart LR
user[👩🏽‍💻 Developer] -->|构建 🚀| runtime[⚙️ Runtime]
runtime --> result[成功]`,
			width: 80,
		},
		{
			name: "narrow branching fallback",
			source: `flowchart TD
A[Choose deployment target] -->|production| B[Production cluster]
A -->|staging| C[Staging cluster]
B --> D[Run health checks]
C --> D`,
			width: 24,
		},
		{
			name: "flowchart ignored style directives",
			source: `flowchart LR
A[Styled source] --> B[Styled target]
classDef important fill:#f96
class A important
style B stroke:#333
click A "https://example.com"
linkStyle 0 stroke-width:2px`,
			width: 60,
		},
		{
			name: "sequence diagram",
			source: `sequenceDiagram
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
S->>C: ACK, Ack = next expected byte`,
			width: 100,
		},
		{
			name: "sequence multiple participants",
			source: `sequenceDiagram
actor U as User
participant W as Web App
participant A as API
participant D as Database
U->>W: Submit form
W->>A: POST /items
A->>D: Save item
D-->>A: Item saved
A-->>W: 201 Created
W-->>U: Show success`,
			width: 100,
		},
		{
			name: "sequence self message",
			source: `sequenceDiagram
participant W as Worker
participant Q as Queue
W->>W: Validate payload
W->>Q: Publish job
Q-->>W: Accepted`,
			width: 60,
		},
		{
			name: "sequence narrow linear fallback",
			source: `sequenceDiagram
participant C as Client application
participant S as Remote server
C->>S: Send a request with a long description
S-->>C: Return a detailed response`,
			width: 24,
		},
		{
			name: "sequence notes only",
			source: `sequenceDiagram
participant A as Service A
participant B as Service B
Note left of A: Before
Note over A,B: Waiting for work
Note right of B: After`,
			width: 60,
		},
		{
			name: "state lifecycle and cycle",
			source: `stateDiagram-v2
state "Processing request" as Processing
[*] --> Idle
Idle --> Processing: begin
Processing --> Waiting: retry
Waiting --> Processing: resume
Processing --> Idle: reset
Processing --> [*]: complete`,
			width: 80,
		},
		{
			name: "state narrow fallback",
			source: `stateDiagram
[*] --> AwaitingInput
AwaitingInput --> ProcessingLongRunningRequest: submit
ProcessingLongRunningRequest --> AwaitingInput: retry
ProcessingLongRunningRequest --> [*]: complete`,
			width: 22,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rendered, ok := Render(tt.source, tt.width)
			require.True(t, ok)
			golden.Assert(t, rendered, tt.name+".golden")
		})
	}
}
