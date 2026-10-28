package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func runHostedStage(args []string, stdout, stderr io.Writer) int {
	command := args[0]
	operands := args[1:]
	operation := "freeze"
	if command == "stage" {
		if len(operands) == 0 {
			return usageError(stderr, "usage: tpa stage create|upload|inspect|cancel [options]")
		}
		operation = operands[0]
		operands = operands[1:]
	}
	destination := ""
	if len(operands) > 0 && !strings.HasPrefix(operands[0], "-") {
		destination = operands[0]
		operands = operands[1:]
	}
	flags := flag.NewFlagSet("tpa "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&destination, "output", destination, "Explicit tparun+ssh://tparun@host/repository-id")
	stage := flags.String("stage", "", "32-hex persistent release stage ID")
	manifest := flags.String("manifest", "", "Exact qualified artifact/release manifest JSON")
	parent := flags.String("parent-generation", "auto", "Pinned base generation, auto snapshots current at stage creation")
	input := flags.String("in", "", "Already-built Debian package to upload")
	sshConfig := flags.String("ssh-config", "", "Pinned OpenSSH client configuration")
	gpg := flags.String("gpg", "", "Owner-side signing key selector")
	keyringPath := flags.String("keyring", "", "Owner public verification keyring")
	fingerprint := flags.String("fingerprint", "", "Expected owner signer fingerprint")
	label := flags.String("label", "TPA-Repo", "APT repository label (channels use distinct repository IDs)")
	origin := flags.String("origin", "TPA-Repo", "APT repository origin; preserve existing client policy")
	description := flags.String("repo-description", "", "APT repository description")
	_ = flags.Bool("json", false, "JSON output (always used for stage operations)")
	if err := flags.Parse(operands); err != nil {
		return 2
	}
	if flags.NArg() != 0 || destination == "" || *stage == "" {
		return usageError(stderr, "stage/sign requires --output tparun+ssh://host/repository-id and --stage 32-hex-id")
	}
	var hash string
	var artifacts []aptpackage.HostedStageArtifact
	if operation == "create" || command == "sign" {
		var err error
		hash, artifacts, err = aptpackage.ReadStageArtifactManifest(*manifest)
		if err != nil {
			return commandFailure(stderr, fmt.Sprintf("read approved manifest: %v", err))
		}
	}
	var result aptpackage.HostedStageResult
	var err error
	if command == "sign" {
		public, readErr := os.ReadFile(*keyringPath)
		if readErr != nil {
			return commandFailure(stderr, fmt.Sprintf("read owner public keyring: %v", readErr))
		}
		result, err = aptpackage.FinalizeHostedStage(context.Background(), destination, aptpackage.HostedSignOptions{StageID: *stage, SSHConfig: *sshConfig, ManifestSHA256: hash, Artifacts: artifacts, GPG: *gpg, Keyring: public, Fingerprint: *fingerprint, Repo: aptpackage.RepoConfig{Label: *label, Origin: *origin, Description: *description, Suite: "stable", Codename: "stable", Components: "main"}})
	} else {
		if operation != "create" && operation != "upload" && operation != "inspect" && operation != "cancel" {
			return usageError(stderr, "stage operation must be create, upload, inspect, or cancel")
		}
		result, err = aptpackage.StageRepositoryArtifacts(context.Background(), destination, operation, aptpackage.HostedStageOptions{StageID: *stage, SSHConfig: *sshConfig, ParentGeneration: *parent, ManifestSHA256: hash, Artifacts: artifacts, File: *input})
	}
	if err != nil {
		return commandFailure(stderr, fmt.Sprintf("%s: %v (retain stage identity %s for inspection/retry)", command, err, *stage))
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(result); err != nil {
		return commandFailure(stderr, err.Error())
	}
	return 0
}
