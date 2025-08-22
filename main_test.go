package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestCLI_SortYaml(t *testing.T) {
	tests := map[string]struct {
		content  string
		paths    []string
		sortType string
		expected string
	}{
		"DefaultMapping_SortsKeysAlphanumerically": {
			paths:    []string{"."},
			sortType: "alphanumeric",
			content: `
name: John Doe
age: 30
is_student: false
gpa: 3.85
`,
			expected: `
age: 30
gpa: 3.85
is_student: false
name: John Doe
`,
		},
		"HumanMapping_SortsKeysNicely": {
			paths:    []string{"."},
			sortType: "human",
			content: `
name: John Doe
age: 30
is_student: false
gpa: 3.85
`,
			expected: `
name: John Doe
age: 30
gpa: 3.85
is_student: false
`,
		},
		"DefaultSequence_SortsElementsAlphanumerically": {
			paths:    []string{"."},
			sortType: "alphanumeric",
			content: `
- Banana
- Strawberry
- Apple
- Orange
`,
			expected: `
- Apple
- Banana
- Orange
- Strawberry
`,
		},
		"DefaultSequenceObjects_SortsElementsByFirstFieldAlphanumerically": {
			paths:    []string{"."},
			sortType: "alphanumeric",
			content: `
- name: Banana
  price: 30
  colour: Yellow
- name: Strawberry
  price: 10
  colour: Red
- name: Apple
  price: 20
  colour: Red
- name: Orange
  price: 30
  colour: Orange
`,
			expected: `
- name: Apple
  price: 20
  colour: Red
- name: Banana
  price: 30
  colour: Yellow
- name: Orange
  price: 30
  colour: Orange
- name: Strawberry
  price: 10
  colour: Red
`,
		},
		"DefaultSequenceObjectAtIndex_SortsObjectAtIndexAlphanumerically": {
			paths:    []string{".[2]"},
			sortType: "alphanumeric",
			content: `
- name: Banana
  price: 30
  colour: Yellow
- name: Strawberry
  price: 10
  colour: Red
- name: Apple
  price: 20
  colour: Red
- name: Orange
  price: 30
  colour: Orange
`,
			expected: `
- name: Banana
  price: 30
  colour: Yellow
- name: Strawberry
  price: 10
  colour: Red
- colour: Red
  name: Apple
  price: 20
- name: Orange
  price: 30
  colour: Orange
`,
		},
		"DefaultSequenceAllObjects_SortsAllObjectsInSequenceAlphanumerically": {
			paths:    []string{".[*]"},
			sortType: "alphanumeric",
			content: `
- name: Banana
  price: 30
  colour: Yellow
- name: Strawberry
  price: 10
  colour: Red
- name: Apple
  price: 20
  colour: Red
- name: Orange
  price: 30
  colour: Orange
`,
			expected: `
- colour: Yellow
  name: Banana
  price: 30
- colour: Red
  name: Strawberry
  price: 10
- colour: Red
  name: Apple
  price: 20
- colour: Orange
  name: Orange
  price: 30
`,
		},
		"HumanSequenceAllObjects_SortsAllObjectsInSequenceNicely": {
			paths:    []string{".[*]"},
			sortType: "human",
			content: `
- name: Banana
  price: 30
  colour: Yellow
- name: Strawberry
  price: 10
  colour: Red
- name: Apple
  price: 20
  colour: Red
- name: Orange
  price: 30
  colour: Orange
`,
			expected: `
- name: Banana
  colour: Yellow
  price: 30
- name: Strawberry
  colour: Red
  price: 10
- name: Apple
  colour: Red
  price: 20
- name: Orange
  colour: Orange
  price: 30
`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			trim := func(s string) string { return strings.TrimPrefix(s, "\n") }
			root := decodeYAML(t, trim(tt.content))
			cli := CLI{YamlPaths: tt.paths, SortType: tt.sortType}

			err := cli.SortYaml(root)
			assert.NoError(t, err)

			output := encodeYAML(t, root)

			assert.Equal(t, trim(tt.expected), output)
		})
	}
}

// decodeYAML decodes a YAML document string into a *yaml.Node (DocumentNode)
func decodeYAML(t *testing.T, s string) *yaml.Node {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(s))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		t.Fatalf("failed to decode YAML: %v", err)
	}
	return &root
}

// encodeYAML encodes a *yaml.Node with fixed indent for debug messages
func encodeYAML(t *testing.T, n *yaml.Node) string {
	t.Helper()
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(n)
	_ = enc.Close()
	return buf.String()
}
