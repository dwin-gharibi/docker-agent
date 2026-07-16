package mermaid

import (
	"slices"
	"strings"
)

type mermaidCursor struct {
	input string
	pos   int
}

// Parse parses a supported Mermaid diagram into a document model.
func Parse(source string) (Document, error) {
	statements := splitMermaidStatements(source)
	if len(statements) == 0 {
		return Document{}, ErrInvalidDiagram
	}

	doc := Document{Nodes: make(map[string]Node)}
	header := strings.Fields(statements[0])
	if len(header) == 0 {
		return Document{}, ErrInvalidDiagram
	}
	switch strings.ToLower(header[0]) {
	case "graph", "flowchart":
		doc.Kind = DiagramFlowchart
		if len(header) > 1 {
			doc.Direction = strings.ToUpper(header[1])
		}
		parseFlowchart(&doc, statements[1:])
	case "sequencediagram":
		doc.Kind = DiagramSequence
		parseSequence(&doc, statements[1:])
	case "statediagram", "statediagram-v2":
		doc.Kind = DiagramState
		parseState(&doc, statements[1:])
	default:
		return Document{}, ErrUnsupportedDiagram
	}

	if len(doc.Edges) == 0 && len(doc.NodeOrder) == 0 {
		return Document{}, ErrInvalidDiagram
	}
	if doc.Kind == DiagramSequence && len(doc.SequenceEvents) == 0 {
		return Document{}, ErrInvalidDiagram
	}
	return doc, nil
}

func splitMermaidStatements(source string) []string {
	var statements []string
	start := 0
	quote := rune(0)
	var stack []rune
	runes := []rune(source)
	flush := func(end int) {
		statement := strings.TrimSpace(string(runes[start:end]))
		if statement != "" && !strings.HasPrefix(statement, "%%") {
			statements = append(statements, statement)
		}
	}

	for i, r := range runes {
		if quote != 0 {
			if r == quote && !isMermaidRuneEscaped(runes, i) {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '`':
			quote = r
		case '[', '(', '{':
			stack = append(stack, r)
		case ']', ')', '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case ';', '\n':
			if len(stack) == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(runes))
	return statements
}

type mermaidParsedNode struct {
	id    string
	Label string
	shape NodeShape
}

func (c *mermaidCursor) readNode() (mermaidParsedNode, bool) {
	id, ok := c.readIdentifier()
	if !ok {
		return mermaidParsedNode{}, false
	}
	label := id
	c.skipSpace()
	if c.eof() {
		return mermaidParsedNode{id: id, Label: label}, true
	}

	var open, closing string
	var shape NodeShape
	switch {
	case c.hasPrefix("(("):
		open, closing, shape = "((", "))", ShapeCircle
	case c.hasPrefix("(["):
		open, closing, shape = "([", "])", ShapeStadium
	case c.hasPrefix("[["):
		open, closing, shape = "[[", "]]", ShapeSubroutine
	case c.hasPrefix("[("):
		open, closing, shape = "[(", ")]", ShapeCylinder
	case c.hasPrefix("{{"):
		open, closing, shape = "{{", "}}", ShapeHexagon
	case c.hasPrefix("["):
		open, closing, shape = "[", "]", ShapeRectangle
	case c.hasPrefix("("):
		open, closing, shape = "(", ")", ShapeRounded
	case c.hasPrefix("{"):
		open, closing, shape = "{", "}", ShapeDecision
	case c.hasPrefix(">"):
		open, closing, shape = ">", "]", ShapeDefault
	default:
		return mermaidParsedNode{id: id, Label: label}, true
	}

	content, ok := c.readDelimited(open, closing)
	if !ok {
		return mermaidParsedNode{}, false
	}
	label = cleanMermaidLabel(content)
	return mermaidParsedNode{id: id, Label: label, shape: shape}, true
}

func (c *mermaidCursor) readDelimited(open, closing string) (string, bool) {
	c.skipSpace()
	if !c.hasPrefix(open) {
		return "", false
	}
	c.pos += len(open)
	start := c.pos
	quote := byte(0)
	for !c.eof() {
		if quote != 0 {
			if c.input[c.pos] == quote && !isMermaidByteEscaped(c.input, c.pos) {
				quote = 0
			}
			c.pos++
			continue
		}
		if c.input[c.pos] == '"' || c.input[c.pos] == '`' {
			quote = c.input[c.pos]
			c.pos++
			continue
		}
		if c.hasPrefix(closing) {
			content := c.input[start:c.pos]
			c.pos += len(closing)
			return content, true
		}
		c.pos++
	}
	return "", false
}

func (c *mermaidCursor) readIdentifier() (string, bool) {
	c.skipSpace()
	start := c.pos
	for !c.eof() {
		if c.pos > start && c.startsEdgeOperator() {
			break
		}
		ch := c.input[c.pos]
		if !isMermaidIdentifierByte(ch) {
			break
		}
		c.pos++
	}
	return c.input[start:c.pos], c.pos > start
}

func isMermaidIdentifierByte(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-'
}

var EdgeOperators = [...]string{"-.->", "-->>", "-->", "==>", "---", "->>", "--x", "--o", "->"}

func (c *mermaidCursor) startsEdgeOperator() bool {
	for _, operator := range EdgeOperators {
		if c.hasPrefix(operator) {
			return true
		}
	}
	return false
}

func (c *mermaidCursor) readEdgeOperator() (string, bool) {
	c.skipSpace()
	for _, operator := range EdgeOperators {
		if c.hasPrefix(operator) {
			c.pos += len(operator)
			return operator, true
		}
	}
	return "", false
}

func (c *mermaidCursor) readPipeLabel() (string, bool) {
	c.skipSpace()
	if !c.consume('|') {
		return "", false
	}
	start := c.pos
	quote := byte(0)
	for !c.eof() {
		ch := c.input[c.pos]
		if quote != 0 {
			if ch == quote && !isMermaidByteEscaped(c.input, c.pos) {
				quote = 0
			}
			c.pos++
			continue
		}
		if ch == '"' || ch == '`' {
			quote = ch
			c.pos++
			continue
		}
		if ch == '|' {
			label := c.input[start:c.pos]
			c.pos++
			return label, true
		}
		c.pos++
	}
	return "", false
}

func (c *mermaidCursor) readStateReference() (string, bool) {
	c.skipSpace()
	if c.hasPrefix("[*]") {
		c.pos += len("[*]")
		return "[*]", true
	}
	return c.readIdentifier()
}

func (c *mermaidCursor) readQuoted() (string, bool) {
	c.skipSpace()
	if c.eof() || c.input[c.pos] != '"' {
		return "", false
	}
	c.pos++
	start := c.pos
	for !c.eof() {
		if c.input[c.pos] == '"' && !isMermaidByteEscaped(c.input, c.pos) {
			value := c.input[start:c.pos]
			c.pos++
			return value, true
		}
		c.pos++
	}
	return "", false
}

func (c *mermaidCursor) consumeKeyword(keyword string) bool {
	position := c.pos
	value, ok := c.readIdentifier()
	if !ok || !strings.EqualFold(value, keyword) {
		c.pos = position
		return false
	}
	return true
}

func (c *mermaidCursor) consume(ch byte) bool {
	c.skipSpace()
	if c.eof() || c.input[c.pos] != ch {
		return false
	}
	c.pos++
	return true
}

func (c *mermaidCursor) remaining() string {
	c.skipSpace()
	value := c.input[c.pos:]
	c.pos = len(c.input)
	return value
}

func isMermaidRuneEscaped(input []rune, position int) bool {
	backslashes := 0
	for i := position - 1; i >= 0 && input[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func isMermaidByteEscaped(input string, position int) bool {
	backslashes := 0
	for i := position - 1; i >= 0 && input[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func (c *mermaidCursor) skipSpace() {
	for !c.eof() && (c.input[c.pos] == ' ' || c.input[c.pos] == '\t' || c.input[c.pos] == '\r') {
		c.pos++
	}
}

func (c *mermaidCursor) hasPrefix(prefix string) bool {
	return strings.HasPrefix(c.input[c.pos:], prefix)
}

func (c *mermaidCursor) eof() bool {
	return c.pos >= len(c.input)
}

func addMermaidNode(doc *Document, id, label string, shapes ...NodeShape) {
	shape := ShapeDefault
	if len(shapes) > 0 {
		shape = shapes[0]
	}
	current, exists := doc.Nodes[id]
	if !exists || label != id {
		doc.Nodes[id] = Node{ID: id, Label: label, Shape: shape}
	} else if current.Shape == ShapeDefault && shape != ShapeDefault {
		current.Shape = shape
		doc.Nodes[id] = current
	}
	addMermaidOrder(doc, id)
}

func addMermaidOrder(doc *Document, id string) {
	doc.NodeOrder = appendUniqueMermaid(doc.NodeOrder, id)
}

func appendUniqueMermaid(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func cleanMermaidLabel(label string) string {
	label = strings.TrimSpace(label)
	if len(label) >= 2 {
		first, last := label[0], label[len(label)-1]
		if first == last && (first == '"' || first == '\'' || first == '`') {
			label = strings.TrimSpace(label[1 : len(label)-1])
		}
	}
	label = strings.ReplaceAll(label, "<br/>", " ")
	label = strings.ReplaceAll(label, "<br>", " ")
	return label
}
