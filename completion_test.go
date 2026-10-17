package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCompletionCommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"completion", shell}, strings.NewReader(""), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d stderr=%s", code, &stderr)
			}
			first := stdout.String()
			for _, command := range publicCommands {
				if !strings.Contains(first, command.Name) {
					t.Fatalf("missing command %s", command.Name)
				}
			}
			stdout.Reset()
			if err := writeCompletion(&stdout, shell); err != nil || stdout.String() != first {
				t.Fatal("completion output is not deterministic")
			}
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s not installed: syntax check skipped", shell)
			}
			path := filepath.Join(t.TempDir(), "completion")
			if err := os.WriteFile(path, []byte(first), 0644); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(binary, "-n", path).CombinedOutput(); err != nil {
				t.Fatalf("shell syntax: %v: %s", err, output)
			}
		})
	}
	for _, args := range [][]string{{"completion"}, {"completion", "powershell"}, {"completion", "bash", "extra"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage: tpa completion") {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	var stderr bytes.Buffer
	if code := run([]string{"completion", "bash"}, strings.NewReader(""), brokenCompletionWriter{}, &stderr); code != 1 {
		t.Fatalf("output failure code=%d", code)
	}
}

type brokenCompletionWriter struct{}

func (brokenCompletionWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

// Standalone completion flags must exist in our parser. Delegated account flags
// belong to the separately qualified customer-ssh CLI, not main.go's FlagSet.
func TestCompletionCatalogMatchesParser(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		owner, ok := method.X.(*ast.Ident)
		if !ok || owner.Name != "flags" {
			return true
		}
		index := 0
		switch method.Sel.Name {
		case "StringVar", "BoolVar", "IntVar", "Int64Var":
			index = 1
		case "String", "Bool", "Int", "Int64":
		default:
			return true
		}
		literal, ok := call.Args[index].(*ast.BasicLit)
		if ok {
			name, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			declared[name] = true
		}
		return true
	})
	covered := map[string]bool{}
	for command, options := range completionOptions {
		if !isPublicCommand(command) {
			t.Fatalf("unknown completion command %s", command)
		}
		for _, option := range options {
			if command == "register" || command == "login" {
				delegated := map[string]bool{"address": true, "known-hosts": true, "identity": true, "server": true}
				if command == "register" {
					delegated["confirm"] = true
					delegated["confirmation-file"] = true
				}
				if !delegated[option.name] {
					t.Fatalf("%s: unknown delegated flag %s", command, option.name)
				}
				continue
			}
			if !declared[option.name] {
				t.Fatalf("%s: unknown parser flag %s", command, option.name)
			}
			covered[option.name] = true
		}
	}
	for name := range declared {
		if !covered[name] {
			t.Errorf("parser flag %s has no completion", name)
		}
	}
}

func TestBashCompletionBehavior(t *testing.T) {
	if _, err := os.Stat("/usr/share/bash-completion/bash_completion"); err != nil {
		t.Skip("bash-completion not installed")
	}
	root := t.TempDir()
	path := filepath.Join(root, "completion.bash")
	var script bytes.Buffer
	if err := writeCompletion(&script, "bash"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, script.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "input space"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package space.deb"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ line, words, expected string }{
		{"tpa pa", "tpa pa", "pack"},
		{"tpa pack --ato", "tpa pack --ato", "--atomic-publish"},
		{"tpa pack -ato", "tpa pack -ato", "-atomic-publish"},
		{"tpa completion f", "tpa completion f", "fish"},
		{"tpa init --arch ar", "tpa init --arch ar", "arm64"},
		{"tpa pack --in inp", "tpa pack --in inp", "input space"},
		{"tpa pack --in=inp", "tpa pack --in=inp", "--in=input space"},
		{"tpa parse -in package", "tpa parse -in package", "package space.deb"},
		{"tpa delete --y", "tpa delete --y", "--yes"},
	}
	for _, test := range cases {
		t.Run(test.line, func(t *testing.T) {
			code := `source /usr/share/bash-completion/bash_completion
source "$1"
COMP_WORDS=(` + test.words + `)
COMP_CWORD=$((${#COMP_WORDS[@]}-1))
COMP_LINE="$2"
COMP_POINT=${#COMP_LINE}
_tpa
printf '%s\n' "${COMPREPLY[@]}"
`
			cmd := exec.Command("bash", "--noprofile", "--norc", "-c", code, "test", path, test.line)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), test.expected) {
				t.Fatalf("%v: completions=%s expected=%s", err, output, test.expected)
			}
		})
	}
}

func TestFishCompletionBehavior(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	path := filepath.Join(t.TempDir(), "tpa.fish")
	var script bytes.Buffer
	if err := writeCompletion(&script, "fish"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, script.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ line, want string }{
		{"tpa pa", "pack"}, {"tpa pack --ato", "--atomic-publish"},
		{"tpa completion f", "fish"}, {"tpa init --arch ar", "arm64"},
		{"tpa delete --y", "--yes"},
		{"tpa pack --gpg ", ""}, {"tpa pack --workers ", ""},
		{"tpa co", "compose"}, {"tpa compose --mem", "--memory-mib"},
		{"tpa compose --memory-mib ", ""}, {"tpa compose --epoch ", ""},
	} {
		cmd := exec.Command(fish, "--no-config", "-c", `source $argv[1]; complete -C $argv[2]`, path, test.line)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil || stderr.Len() != 0 || !strings.Contains(stdout.String(), test.want) || (test.want == "" && strings.TrimSpace(stdout.String()) != "") {
			t.Fatalf("%s: err=%v stdout=%s stderr=%s", test.line, err, &stdout, &stderr)
		}
	}
}
