package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"gopkg.in/yaml.v3"
)

// CLI represents the command-line interface structure
type CLI struct {
	InputFile string   `name:"file" short:"f" help:"Input YAML file path" type:"existingfile" required:""`
	YamlPaths []string `name:"yaml-path" short:"p" help:"YAML path(s) in dot notation. Optional trailing bracket selector for sequences: 'path' sorts the sequence by first field; 'path[*]' sorts keys of all elements; 'path[0]' sorts keys of element 0. You can still use numeric tokens to index sequences in the middle, e.g. 'servers.0.roles'. Repeat -p to process multiple paths in order." required:""`
	Write     bool     `name:"write" short:"w" help:"Write changes back to the input file instead of printing to stdout"`
	SortType  string   `name:"sort" short:"t" help:"Sort type for mapping keys: 'alphanumeric' (default) or 'human' (common keys first, then the rest alphanumeric)" enum:"alphanumeric,human" default:"alphanumeric"`
	Verbose   bool     `name:"verbose" short:"v" help:"Verbose output"`
}

const description = `Yet Another Yaml Sorter`

func main() {
	var cli CLI
	_ = kong.Parse(&cli,
		kong.Name("yays"),
		kong.Description(description),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)

	// Read
	doc, indent, err := cli.ReadFile()
	if err != nil {
		fail(err)
	}

	// Perform Sort
	if err := cli.SortYaml(doc); err != nil {
		fail(err)
	}

	// Print sorted yaml
	if cli.Verbose || !cli.Write {
		if err := PrintYaml(doc, indent); err != nil {
			fail(err)
		}
	}

	// Save sorted yaml
	if cli.Write {
		if err := cli.WriteYaml(doc, indent); err != nil {
			fail(err)
		}
	}
}

func PrintYaml(doc *yaml.Node, indent int) error {
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(indent)
	err := enc.Encode(doc)
	_ = enc.Close()
	if err != nil {
		return fmt.Errorf("failed to encode YAML: %v", err)
	}
	return nil
}

func (cli CLI) WriteYaml(doc *yaml.Node, indent int) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	err := enc.Encode(doc)
	_ = enc.Close()
	if err != nil {
		return fmt.Errorf("failed to encode YAML: %v", err)
	}
	info, err := os.Stat(cli.InputFile)
	if err != nil {
		return fmt.Errorf("failed to stat input file: %v", err)
	}
	perm := info.Mode().Perm()
	if err := os.WriteFile(cli.InputFile, buf.Bytes(), perm); err != nil {
		return fmt.Errorf("failed to write back to file: %v", err)
	}
	return nil
}

func (cli CLI) rankSortType(key string) int {
	humanCommonOrder := []string{"apiVersion", "kind", "metadata", "name", "namespace", "labels", "annotations", "id", "version"}
	switch cli.SortType {
	case "alphanumeric":
		return len(humanCommonOrder) + 1
	case "human":
		for i, k := range humanCommonOrder {
			if key == k {
				return i
			}
		}
		return len(humanCommonOrder) + 1
	default:
		// Fallback to alphanumeric behavior for unknown sort types
		return len(humanCommonOrder) + 1
	}
}

func (cli CLI) SortYaml(doc *yaml.Node) error {
	// Navigate and sort for each provided path, in order
	for _, path := range cli.YamlPaths {
		baseTokens, sel, selIdx, err := parsePathSelection(path)
		if err != nil {
			return fmt.Errorf("invalid path %q: %v", path, err)
		}
		target, err := navigateToPath(doc, baseTokens)
		if err != nil {
			return fmt.Errorf("failed to navigate to path %q: %v", path, err)
		}

		switch sel {
		case selNone:
			// Default behavior at the target level
			switch target.Kind {
			case yaml.MappingNode:
				cli.sortMappingNodeKeys(target)
			case yaml.SequenceNode:
				// Only sort the sequence by the first field
				sortSequenceByFirstField(target)
			default:
				return fmt.Errorf("target at path %q must be a mapping or sequence (got kind=%d)", path, target.Kind)
			}
		case selIndex:
			// Sort keys of a specific element in a sequence
			if target.Kind != yaml.SequenceNode {
				return fmt.Errorf("path %q selection by index requires a sequence target (got kind=%d)", path, target.Kind)
			}
			if selIdx < 0 || selIdx >= len(target.Content) {
				return fmt.Errorf("index %d out of range for path %q (len=%d)", selIdx, path, len(target.Content))
			}
			el := target.Content[selIdx]
			if el.Kind == yaml.MappingNode {
				cli.sortMappingNodeKeys(el)
			}
		case selAll:
			// Sort keys of all elements in a sequence
			if target.Kind != yaml.SequenceNode {
				return fmt.Errorf("path %q selection [*] requires a sequence target (got kind=%d)", path, target.Kind)
			}
			for _, el := range target.Content {
				if el.Kind == yaml.MappingNode {
					cli.sortMappingNodeKeys(el)
				}
			}
		}
	}
	return nil
}

func (cli CLI) ReadFile() (*yaml.Node, int, error) {
	f, err := os.Open(cli.InputFile)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open file: %v", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, 0, fmt.Errorf("failed to decode YAML: %v", err)
	}

	// Detect indentation from original file contents
	data, err := os.ReadFile(cli.InputFile)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read input file: %v", err)
	}
	return &root, detectIndentation(data), nil
}

func tokenizePath(path string) []string {
	p := strings.TrimSpace(path)
	if p == "" || p == "." {
		return nil
	}
	// Simple dot-split tokenization; numeric tokens are treated as sequence indices.
	return strings.Split(p, ".")
}

// selection indicates optional bracket selection at the end of a path
// selNone: no bracket; selIndex: [N]; selAll: [*]
type selection int

const (
	selNone selection = iota
	selIndex
	selAll
)

// parsePathSelection parses an input path which may optionally end with a bracket selector
// like [0] or [*]. It returns the base dot-path tokens (without the selector), the selection type,
// and the index (for selIndex) or -1.
func parsePathSelection(path string) ([]string, selection, int, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil, selNone, -1, nil
	}
	if strings.HasSuffix(p, "]") {
		lb := strings.LastIndex(p, "[")
		if lb == -1 {
			return nil, selNone, -1, fmt.Errorf("invalid path, unmatched ']' in %q", p)
		}
		inside := strings.TrimSpace(p[lb+1 : len(p)-1])
		base := strings.TrimSpace(p[:lb])
		if inside == "*" {
			return tokenizePath(base), selAll, -1, nil
		}
		if n, err := strconv.Atoi(inside); err == nil {
			return tokenizePath(base), selIndex, n, nil
		}
		return nil, selNone, -1, fmt.Errorf("invalid bracket selection %q", inside)
	}
	return tokenizePath(p), selNone, -1, nil
}

func navigateToPath(root *yaml.Node, tokens []string) (*yaml.Node, error) {
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if len(tokens) == 0 {
		return n, nil
	}

	for _, tok := range tokens {
		switch n.Kind {
		case yaml.MappingNode:
			found := false
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Value == tok {
					n = n.Content[i+1]
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("key %q not found in mapping", tok)
			}
		case yaml.SequenceNode:
			idx, err := strconv.Atoi(tok)
			if err != nil {
				return nil, fmt.Errorf("expected numeric index into sequence, got %q", tok)
			}
			if idx < 0 || idx >= len(n.Content) {
				return nil, fmt.Errorf("index %d out of range [0,%d)", idx, len(n.Content))
			}
			n = n.Content[idx]
		default:
			return nil, fmt.Errorf("cannot descend into node kind=%d at token %q", n.Kind, tok)
		}
	}
	return n, nil
}

func (cli CLI) sortMappingNodeKeys(n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		return
	}
	type kv struct {
		k *yaml.Node
		v *yaml.Node
	}
	pairs := make([]kv, 0, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		pairs = append(pairs, kv{k: n.Content[i], v: n.Content[i+1]})
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		ri, rj := cli.rankSortType(pairs[i].k.Value), cli.rankSortType(pairs[j].k.Value)
		if ri != rj {
			return ri < rj
		}
		return pairs[i].k.Value < pairs[j].k.Value
	})

	newContent := make([]*yaml.Node, 0, len(pairs)*2)
	for _, p := range pairs {
		newContent = append(newContent, p.k, p.v)
	}
	n.Content = newContent
}

// sortSequenceByFirstField sorts a sequence's items by the value of the first field
// within each item if the item is a mapping. For non-mapping items, it falls back
// to a comparable string for the entire item.
func sortSequenceByFirstField(n *yaml.Node) {
	if n.Kind != yaml.SequenceNode {
		return
	}
	type item struct {
		key  string
		node *yaml.Node
	}
	items := make([]item, len(n.Content))
	for i, el := range n.Content {
		items[i] = item{
			key:  firstFieldComparableValue(el),
			node: el,
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].key < items[j].key
	})
	for i := range items {
		n.Content[i] = items[i].node
	}
}

// firstFieldComparableValue returns the comparable string of the first field's value
// if the element is a mapping; otherwise, it returns a comparable string for the element.
func firstFieldComparableValue(el *yaml.Node) string {
	if el == nil {
		return ""
	}
	if el.Kind == yaml.MappingNode && len(el.Content) >= 2 {
		// First key-value pair: Content[0] is key, Content[1] is value
		return nodeComparableString(el.Content[1])
	}
	return nodeComparableString(el)
}

// nodeComparableString produces a deterministic string representation suitable for sorting.
func nodeComparableString(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		var b strings.Builder
		b.WriteString("{")
		for i := 0; i < len(n.Content); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(n.Content[i].Value)
			b.WriteByte(':')
			b.WriteString(nodeComparableString(n.Content[i+1]))
		}
		b.WriteString("}")
		return b.String()
	case yaml.SequenceNode:
		var b strings.Builder
		b.WriteString("[")
		for i := 0; i < len(n.Content); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(nodeComparableString(n.Content[i]))
		}
		b.WriteString("]")
		return b.String()
	default:
		return ""
	}
}

// detectIndentation inspects the original YAML text and tries to infer the indentation width (spaces per level).
// It collects non-zero counts of leading spaces across lines and returns their GCD. Defaults to 2 on failure.
func detectIndentation(data []byte) int {
	lines := bytes.Split(data, []byte("\n"))
	indent := 0
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		// Skip lines starting with tabs; yaml.Encoder does not support tab indentation.
		if line[0] == '\t' {
			continue
		}
		// Count leading spaces.
		i := 0
		for i < len(line) && line[i] == ' ' {
			i++
		}
		// Ignore lines with zero leading spaces (top-level) or mixed space+tab at indent boundary.
		if i == 0 {
			continue
		}
		if i < len(line) && line[i] == '\t' {
			continue
		}
		if indent == 0 {
			indent = i
		} else {
			indent = gcd(indent, i)
		}
	}
	if indent <= 0 {
		return 2
	}
	return indent
}

func gcd(a, b int) int {
	// Normalize to non-negative
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	// Euclidean algorithm
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
