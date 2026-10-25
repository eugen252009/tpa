package releaseworkflow

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var releaseEnvironmentRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func validateReleaseBranchName(branch string) error {
	if branch == "" {
		return nil
	}
	if strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "refs/") {
		return fmt.Errorf("release.source_branch must be a short Git branch name")
	}
	if err := exec.Command("git", "check-ref-format", "refs/heads/"+branch).Run(); err != nil {
		return fmt.Errorf("release.source_branch is not a valid Git branch name: %w", err)
	}
	return nil
}

// A release may remain eligible as staging advances normally. Missing remote
// history or a rewritten branch excluding the candidate fails closed. Callers
// must supply a fresh, complete origin checkout; a local branch is not authority.
func validateReleaseSourceBranch(workspace, branch, commit string) error {
	if branch == "" {
		return nil
	}
	if err := validateReleaseBranchName(branch); err != nil {
		return err
	}
	tip, err := gitOutput(workspace, "rev-parse", "--verify", "refs/remotes/origin/"+branch+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve release source branch origin/%s (fetch complete remote history): %w", branch, err)
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", commit, tip)
	cmd.Dir = workspace
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("release commit %s must be contained in origin/%s: %w", commit, branch, err)
	}
	return nil
}

// VerifyReleaseEnvironment checks policy, not an approval receipt. GitHub's
// environment scheduler supplies the actual approval gate for the publish job.
// This read-only check rejects absent/unprotected environments before expensive
// builds, and is repeated immediately before the publisher can mutate a release.
func VerifyReleaseEnvironment(repository, environment string) error {
	if !githubRepositoryRE.MatchString(repository) || strings.Contains(repository, "..") {
		return fmt.Errorf("GitHub repository must have owner/name form")
	}
	if !releaseEnvironmentRE.MatchString(environment) {
		return fmt.Errorf("invalid publication environment name")
	}
	output, err := ghOutput("api", "--method", "GET", "repos/"+repository+"/environments/"+environment)
	if err != nil {
		return fmt.Errorf("read protected publication environment %s: %w", environment, err)
	}
	var policy struct {
		Name            string `json:"name"`
		CanAdminsBypass *bool  `json:"can_admins_bypass"`
		ProtectionRules []struct {
			Type              string `json:"type"`
			PreventSelfReview bool   `json:"prevent_self_review"`
			Reviewers         []struct {
				Type     string `json:"type"`
				Reviewer struct {
					ID int64 `json:"id"`
				} `json:"reviewer"`
			} `json:"reviewers"`
		} `json:"protection_rules"`
	}
	if err := json.Unmarshal([]byte(output), &policy); err != nil {
		return fmt.Errorf("decode publication environment policy: %w", err)
	}
	if !strings.EqualFold(policy.Name, environment) || policy.CanAdminsBypass == nil || *policy.CanAdminsBypass {
		return fmt.Errorf("publication environment %s must exist and disallow administrator bypass", environment)
	}
	for _, rule := range policy.ProtectionRules {
		if rule.Type != "required_reviewers" || !rule.PreventSelfReview {
			continue
		}
		for _, reviewer := range rule.Reviewers {
			if (reviewer.Type == "User" || reviewer.Type == "Team") && reviewer.Reviewer.ID > 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("publication environment %s must require a reviewer and prevent self-review", environment)
}
