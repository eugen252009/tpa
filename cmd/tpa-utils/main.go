package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eugen252009/tpa/internals/aptpackage"
	"github.com/eugen252009/tpa/pkg/tpautils"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errOut io.Writer) int {
	err := execute(args, out)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
func execute(args []string, out io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		_, err := fmt.Fprintln(out, "tpa-utils check FIELD VALUE | check control definition.json\ntpa-utils version compare VERSION VERSION\ntpa-utils repo inspect [--codename stable --keyring public.gpg] REPOSITORY\ntpa-utils repo html [--out web --base-url https://host/repo/] REPOSITORY")
		return err
	}
	if len(args) == 3 && args[0] == "check" {
		var err error
		if args[1] == "control" {
			defs, e := aptpackage.ReadDefinitions(args[2])
			if e != nil {
				return e
			}
			if len(defs) != 1 {
				return fmt.Errorf("control check requires one definition")
			}
			err = tpautils.ValidateControl(defs[0].Control)
		} else {
			err = tpautils.ValidateField(args[1], args[2])
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "Valid")
		return err
	}
	if len(args) == 4 && args[0] == "version" && args[1] == "compare" {
		n, err := tpautils.CompareVersions(args[2], args[3])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, n)
		return err
	}
	if len(args) >= 3 && args[0] == "repo" && (args[1] == "inspect" || args[1] == "html") {
		fs := flag.NewFlagSet("repo "+args[1], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		codename := fs.String("codename", "stable", "Distribution")
		keyring := fs.String("keyring", "", "Public OpenPGP keyring")
		fingerprint := fs.String("fingerprint", "", "Expected full primary fingerprint")
		required := fs.Bool("require-signed", false, "Require verified InRelease")
		output := fs.String("out", "", "Browser output directory")
		base := fs.String("base-url", "", "Artifact repository URL for separate browser output")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("one repository locator required (options precede the locator)")
		}
		opts := tpautils.RepositoryOptions{Codename: *codename, Fingerprint: *fingerprint, RequireSigned: *required}
		if *keyring != "" {
			f, err := os.Open(*keyring)
			if err != nil {
				return err
			}
			data, readErr := io.ReadAll(io.LimitReader(f, aptpackage.MaxRepositoryKeyringSize+1))
			closeErr := f.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			if len(data) > aptpackage.MaxRepositoryKeyringSize {
				return fmt.Errorf("keyring too large")
			}
			opts.Keyring = data
		}
		if args[1] == "html" {
			return tpautils.GenerateBrowser(context.Background(), fs.Arg(0), tpautils.BrowserOptions{RepositoryOptions: opts, Output: *output, BaseURL: *base})
		}
		if *output != "" || *base != "" {
			return fmt.Errorf("out/base-url are only valid for repo html")
		}
		report, err := tpautils.Inspect(context.Background(), fs.Arg(0), opts)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(report)
	}
	return fmt.Errorf("invalid invocation; run tpa-utils --help")
}
