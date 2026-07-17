package mermaid

import "errors"

var (
	// ErrUnsupportedDiagram indicates that the Mermaid diagram grammar is not supported.
	ErrUnsupportedDiagram = errors.New("unsupported Mermaid diagram")
	// ErrInvalidDiagram indicates that no renderable syntax could be parsed.
	ErrInvalidDiagram = errors.New("invalid Mermaid diagram")
)

// DiagramKind identifies a Mermaid diagram grammar.
type DiagramKind uint8

const (
	// DiagramUnknown represents an unsupported diagram grammar.
	DiagramUnknown DiagramKind = iota
	// DiagramFlowchart represents graph and flowchart diagrams.
	DiagramFlowchart
	// DiagramSequence represents sequence diagrams.
	DiagramSequence
	// DiagramState represents state diagrams.
	DiagramState
)

// NodeShape identifies the visual shape declared for a node.
type NodeShape uint8

const (
	ShapeDefault NodeShape = iota
	ShapeRectangle
	ShapeRounded
	ShapeStadium
	ShapeSubroutine
	ShapeCylinder
	ShapeCircle
	ShapeDecision
	ShapeHexagon
)

// Node is a declared or implicitly referenced diagram node.
type Node struct {
	ID    string
	Label string
	Shape NodeShape
}

// SequenceEventKind identifies an item on a sequence-diagram timeline.
type SequenceEventKind uint8

const (
	SequenceMessage SequenceEventKind = iota
	SequenceNote
)

// NotePlacement identifies where a sequence note is anchored.
type NotePlacement uint8

const (
	NoteOver NotePlacement = iota
	NoteLeftOf
	NoteRightOf
)

// SequenceEvent is a message or note in timeline order.
type SequenceEvent struct {
	Kind      SequenceEventKind
	From      string
	To        string
	Label     string
	Placement NotePlacement
}

// Subgraph groups flowchart nodes and nested subgraphs under a labeled container.
type Subgraph struct {
	ID       string
	Label    string
	ParentID string
	Nodes    []string
}

// Document is the syntax model produced by Parse.
type Document struct {
	Kind           DiagramKind
	Direction      string
	Nodes          map[string]Node
	NodeOrder      []string
	Edges          []Edge
	Subgraphs      []Subgraph
	Participants   []string
	SequenceEvents []SequenceEvent
}

// Edge connects two nodes and optionally carries a display label.
type Edge struct {
	From  string
	To    string
	Label string
}
