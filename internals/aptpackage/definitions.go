package aptpackage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MaxDefinitionBytes = 1 << 20
const MaxDefinitionsBytes = 64 << 20

// ReadDefinitions normalizes one JSON object, JSONL, or recursively sorted
// regular .json files without an intermediate JSONL file. Both encoded input
// and decoded normalized controls are bounded independently.
func ReadDefinitions(input string) ([]Config, error) {
	info, err := os.Lstat(input)
	if err != nil {
		return nil, err
	}
	var paths []string
	if info.IsDir() {
		err = filepath.WalkDir(input, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".json") {
				if !d.Type().IsRegular() {
					return fmt.Errorf("definition is not a regular file: %s", path)
				}
				paths = append(paths, path)
			}
			if len(paths) > maxRepositoryPackageCount {
				return fmt.Errorf("too many definition files")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else if info.Mode().IsRegular() {
		paths = []string{input}
	} else {
		return nil, fmt.Errorf("input must be a regular file or directory, not a symlink")
	}
	sort.Strings(paths)
	var result []Config
	total := 0
	add := func(data []byte, source string) error {
		if len(data) > MaxDefinitionBytes {
			return fmt.Errorf("%s: definition exceeds %d bytes", source, MaxDefinitionBytes)
		}
		if len(result) >= maxRepositoryPackageCount {
			return fmt.Errorf("too many definitions")
		}
		if err := uniqueJSONKeys(data); err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		for key := range raw {
			switch key {
			case "control", "scripts", "provenance", "repo", "indir", "outdir", "gpg":
			default:
				return fmt.Errorf("%s: unsupported definition field %q (external payloads are not supported)", source, key)
			}
		}
		if _, ok := raw["control"]; !ok {
			return fmt.Errorf("%s: control object required", source)
		}
		if scripts, ok := raw["scripts"]; ok {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(scripts, &m); err != nil {
				return err
			}
			for key := range m {
				if key != "preinst" && key != "postinst" && key != "prerm" && key != "postrm" {
					return fmt.Errorf("%s: unknown script %q", source, key)
				}
			}
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		result = append(result, cfg)
		return nil
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		remaining := MaxDefinitionsBytes - total
		data, readErr := io.ReadAll(io.LimitReader(f, int64(remaining)+1))
		closeErr := f.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += len(data)
		if total > MaxDefinitionsBytes {
			return nil, fmt.Errorf("definition input exceeds %d bytes", MaxDefinitionsBytes)
		}
		if strings.EqualFold(filepath.Ext(path), ".jsonl") {
			scanner := bufio.NewScanner(bytes.NewReader(data))
			scanner.Buffer(make([]byte, 4096), MaxDefinitionBytes+1)
			line := 0
			for scanner.Scan() {
				line++
				if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
					return nil, fmt.Errorf("%s:%d: JSONL requires a complete object on every line", path, line)
				}
				if err := add(scanner.Bytes(), fmt.Sprintf("%s:%d", path, line)); err != nil {
					return nil, err
				}
			}
			if err := scanner.Err(); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
		} else if err := add(data, path); err != nil {
			return nil, err
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no definitions found")
	}
	return result, nil
}

// Reject duplicate JSON keys rather than silently changing identity or policy.
func uniqueJSONKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("JSON nesting exceeds 64")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok {
					return fmt.Errorf("invalid JSON key")
				}
				if keys[s] {
					return fmt.Errorf("duplicate JSON key %q", s)
				}
				keys[s] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON definition")
	}
	return nil
}
