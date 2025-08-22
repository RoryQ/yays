# YAYS - Yet Another YAML Sorter

A simple command-line tool for sorting keys and arrays in YAML files using dot notation paths.

## Installation

```bash
go install github.com/roryq/yays@latest
```

## Usage

```
Usage: yays --file=STRING --yaml-path=YAML-PATH,... [flags]

Yet Another Yaml Sorter

Flags:
  -h, --help                       Show context-sensitive help.
  -f, --file=STRING                Input YAML file path
  -p, --yaml-path=YAML-PATH,...    YAML path(s) in dot notation. Optional trailing bracket selector for sequences: 'path' sorts the sequence by first field; 'path[*]' sorts keys of all elements; 'path[0]' sorts keys of
                                   element 0. You can still use numeric tokens to index sequences in the middle, e.g. 'servers.0.roles'. Repeat -p to process multiple paths in order.
  -w, --write                      Write changes back to the input file instead of printing to stdout
  -t, --sort="alphanumeric"        Sort type for mapping keys: 'alphanumeric' (default) or 'human' (common keys first, then the rest alphanumeric)
  -v, --verbose                    Verbose output
```

### Path notation
- Provide a YAML path in dot-notation with -p/--path. You can repeat -p to apply multiple operations in order.
- At the target path:
  - If the node is a mapping (object), its keys are sorted.
  - If the node is a sequence (array):
    - path: sort the sequence by the first field of each element (no per-element key sorting).
    - path[*]: sort keys of all mapping elements in the sequence (preserves sequence order).
    - path[0]: sort keys of the first element (preserves sequence order).
- Sequence indices can still be used to address a specific array element in the middle of a path, e.g. `servers.0.roles`.

### Sort types

- Sorted alphanumerically by default.
- Human-readable option is provided which will prioritise keys like "name", "id", "kind" before sorting alphanumerically.


