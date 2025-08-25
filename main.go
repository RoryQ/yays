package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/earthboundkid/versioninfo/v2"
	"gopkg.in/yaml.v3"
)

// CLI represents the command-line interface structure
type CLI struct {
	InputFile string           `name:"file" short:"f" help:"Input YAML file path" type:"existingfile" required:""`
	YamlPaths []string         `name:"yaml-path" short:"p" help:"YAML path(s) in dot notation. Bracket selectors [*] and [N] can appear at the end or mid-path to loop over sequences or mappings with [*], or index sequences with [N] (e.g., 'items[*].meta', 'servers[0].roles'). At the target: mappings have keys sorted; sequences are sorted by the first field of each element. Repeat -p to process multiple paths in order." required:""`
	Write     bool             `name:"write" short:"w" help:"Write changes back to the input file instead of printing to stdout"`
	SortType  string           `name:"sort" short:"t" help:"Sort type for mapping keys: 'alphanumeric' (default) or 'human' (common keys first, then the rest alphanumeric)" enum:"alphanumeric,human" default:"alphanumeric"`
	Verbose   bool             `name:"verbose" short:"v" help:"Verbose output"`
	Version   kong.VersionFlag `name:"version" short:"V" help:"Print version information and exit" version:"${version}"`
}

const description = `Yet Another Yaml Sorter`

func main() {
	var cli CLI
	_ = kong.Parse(&cli,
		kong.Name("yays"),
		kong.Description(description),
		kong.Vars{"version": versioninfo.Short()},
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
		steps, err := parsePathSteps(path)
		if err != nil {
			return fmt.Errorf("invalid path %q: %v", path, err)
		}
		targets, err := resolveTargets(doc, steps)
		if err != nil {
			return fmt.Errorf("failed to navigate to path %q: %v", path, err)
		}

		looping := stepsContainLoop(steps)
		for _, target := range targets {
			switch target.Kind {
			case yaml.MappingNode:
				cli.sortMappingNodeKeys(target)
			case yaml.SequenceNode:
				// Sort the sequence by the first field
				sortSequenceByFirstField(target)
			default:
				if !looping { // preserve previous behavior: error only when not looping
					return fmt.Errorf("target at path %q must be a mapping or sequence (got kind=%d)", path, target.Kind)
				}
				// when looping, silently skip non-sortable scalars
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

type pathStepKind int

const (
	stepKey pathStepKind = iota
	stepIndex
	stepAll
)

type pathStep struct {
	kind  pathStepKind
	key   string
	index int
}

func parsePathSteps(path string) ([]pathStep, error) {
	p := strings.TrimSpace(path)
	if p == "" || p == "." {
		return nil, nil
	}
	parts := strings.Split(p, ".")
	var steps []pathStep
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" { // skip empty tokens from leading/trailing dots
			continue
		}
		// Bracket-only token: [*] or [0]
		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			inside := strings.TrimSpace(part[1 : len(part)-1])
			if inside == "*" {
				steps = append(steps, pathStep{kind: stepAll})
				continue
			}
			if n, err := strconv.Atoi(inside); err == nil {
				steps = append(steps, pathStep{kind: stepIndex, index: n})
				continue
			}
			return nil, fmt.Errorf("invalid bracket selection %q", inside)
		}
		// Token with suffix brackets: name[*] or name[0]
		if lb := strings.Index(part, "["); lb != -1 && strings.HasSuffix(part, "]") {
			name := part[:lb]
			if name != "" {
				steps = append(steps, pathStep{kind: stepKey, key: name})
			}
			inside := strings.TrimSpace(part[lb+1 : len(part)-1])
			if inside == "*" {
				steps = append(steps, pathStep{kind: stepAll})
				continue
			}
			if n, err := strconv.Atoi(inside); err == nil {
				steps = append(steps, pathStep{kind: stepIndex, index: n})
				continue
			}
			return nil, fmt.Errorf("invalid bracket selection %q", inside)
		}
		steps = append(steps, pathStep{kind: stepKey, key: part})
	}
	return steps, nil
}

func stepsContainLoop(steps []pathStep) bool {
	for _, s := range steps {
		if s.kind == stepAll {
			return true
		}
	}
	return false
}

func resolveTargets(root *yaml.Node, steps []pathStep) ([]*yaml.Node, error) {
	// Start at the document's content root
	cur := make([]*yaml.Node, 0, 1)
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	cur = append(cur, n)

	for _, st := range steps {
		next := make([]*yaml.Node, 0)
		switch st.kind {
		case stepKey:
			for _, node := range cur {
				if node.Kind != yaml.MappingNode {
					return nil, fmt.Errorf("cannot descend into node kind=%d at token %q", node.Kind, st.key)
				}
				found := false
				for i := 0; i < len(node.Content); i += 2 {
					k := node.Content[i]
					if k.Value == st.key {
						next = append(next, node.Content[i+1])
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("key %q not found in mapping", st.key)
				}
			}
		case stepIndex:
			for _, node := range cur {
				if node.Kind != yaml.SequenceNode {
					return nil, fmt.Errorf("expected numeric index into sequence, got index %d with kind=%d", st.index, node.Kind)
				}
				if st.index < 0 || st.index >= len(node.Content) {
					return nil, fmt.Errorf("index %d out of range [0,%d)", st.index, len(node.Content))
				}
				next = append(next, node.Content[st.index])
			}
		case stepAll:
			for _, node := range cur {
				switch node.Kind {
				case yaml.SequenceNode:
					next = append(next, node.Content...)
				case yaml.MappingNode:
					// iterate over all values in the mapping
					for i := 0; i < len(node.Content); i += 2 {
						next = append(next, node.Content[i+1])
					}
				default:
					return nil, fmt.Errorf("selection [*] requires a sequence or mapping target (got kind=%d)", node.Kind)
				}
			}
		}
		cur = next
	}
	return cur, nil
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
