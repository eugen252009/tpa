package main

import (
	"fmt"
	"io"
	"strings"
)

// Completion is deliberately offline: it never opens repositories, executes
// package operations, or contacts a remote host while the user presses Tab.
type completionOption struct {
	name, kind, description string
}

var completionOptions = map[string][]completionOption{
	"init": {
		{"name", "text", "Package name"}, {"ver", "text", "Package version"},
		{"arch", "arch", "Package architecture"}, {"maintainer", "text", "Maintainer"},
		{"desc", "text", "Description"}, {"depends", "text", "Dependencies"},
		{"pre-depends", "text", "Pre-dependencies"}, {"recommends", "text", "Recommendations"},
		{"suggests", "text", "Suggestions"}, {"provides", "text", "Provided packages"},
		{"conflicts", "text", "Conflicts"}, {"breaks", "text", "Breaks"},
		{"replaces", "text", "Replacements"}, {"homepage", "text", "Homepage"},
		{"section", "text", "Section"}, {"priority", "text", "Priority"},
		{"built-using", "text", "Built using"}, {"essential", "yesno", "Essential package"},
		{"multi-arch", "multiarch", "Multi-Arch policy"},
		{"preinst", "text", "Pre-install script body"}, {"postinst", "text", "Post-install script body"},
		{"prerm", "text", "Pre-removal script body"}, {"postrm", "text", "Post-removal script body"},
		{"out", "dir", "Package output directory"}, {"no-provenance", "bool", "Disable automatic provenance"},
	},
	"build": {{"in", "dir", "Package tree"}, {"out", "file", "Output archive or directory"}},
	"check": {{"in", "dir", "Package tree"}, {"json", "bool", "JSON diagnostics"}},
	"compose": {
		{"in", "file", "JSON, JSONL or directory of definitions"}, {"out", "dir", "New local repository"},
		{"workers", "text", "Build worker count"}, {"memory-mib", "text", "Worker scratch budget in MiB"}, {"epoch", "text", "Reproducible Unix timestamp"},
		{"gpg", "text", "Signing identity"}, {"origin", "text", "Repository origin"}, {"label", "text", "Repository label"}, {"suite", "text", "Suite"}, {"codename", "text", "Codename"}, {"components", "text", "Component"}, {"repo-description", "text", "Repository description"},
	},
	"publish": {{"json", "none", "Publish result and transfer counters as JSON"}, {"in", "dir", "Finished signed repository"}, {"out", "text", "Explicit TPA.run SSH destination"}, {"output", "text", "Explicit TPA.run SSH destination"}, {"ssh-config", "file", "OpenSSH config"}, {"keyring", "file", "Owner public key"}, {"fingerprint", "text", "Expected owner signer"}, {"generation-id", "text", "Generation identity"}, {"parent-generation", "text", "Expected parent"}},
	"stage":   {{"output", "text", "Repository SSH-CAS destination"}, {"stage", "text", "Persistent 32-hex stage identity"}, {"manifest", "file", "Qualified artifact manifest"}, {"in", "file", "Already-built Debian archive"}, {"ssh-config", "file", "Pinned SSH configuration"}, {"parent-generation", "text", "Pinned base or auto"}, {"json", "bool", "JSON stage result"}},
	"sign":    {{"output", "text", "Repository SSH-CAS destination"}, {"stage", "text", "Persistent release stage"}, {"manifest", "file", "Approved artifact manifest"}, {"gpg", "text", "Owner signing identity"}, {"keyring", "file", "Owner public trust"}, {"fingerprint", "text", "Expected signer"}, {"ssh-config", "file", "Pinned SSH configuration"}, {"origin", "text", "Existing APT origin"}, {"label", "text", "Existing APT label"}, {"json", "bool", "JSON receipt"}},
	"parse":   {{"in", "file", "Debian archive"}},
	"pack": {
		{"in", "dir", "Package input directory"}, {"out", "dir", "Repository output"},
		{"output", "dir", "Output path or SSH destination"}, {"atomic-publish", "dir", "Atomic local destination"},
		{"empty", "bool", "Initialize an empty repository"}, {"architectures", "text", "Comma-separated architectures"},
		{"origin", "text", "Repository origin"}, {"label", "text", "Repository label"},
		{"suite", "text", "APT suite"}, {"codename", "text", "Distribution codename"},
		{"components", "text", "Repository component"}, {"repo-description", "text", "Repository description"},
		{"gpg", "text", "Signing key fingerprint"}, {"workers", "text", "Worker count"},
		{"ssh-config", "file", "OpenSSH configuration"}, {"generation-manifest", "file", "Generation inventory output"},
		{"repository-id", "text", "Repository identity"}, {"generation-id", "text", "Generation identity"},
		{"parent-generation", "text", "Expected parent generation"},
	},
	"inspect": {
		{"in", "dir", "Repository path or URL"}, {"codename", "text", "Distribution codename"},
		{"package", "text", "Exact package name"}, {"ver", "text", "Exact package version"},
		{"arch", "arch", "Exact architecture"}, {"json", "bool", "JSON output"},
		{"keyring", "file", "Public signing key"}, {"fingerprint", "text", "Expected signer fingerprint"},
		{"require-signed", "bool", "Require signed metadata"},
	},
	"verify": {
		{"in", "dir", "Repository path or URL"}, {"codename", "text", "Distribution codename"},
		{"json", "bool", "JSON output"}, {"keyring", "file", "Public signing key"},
		{"fingerprint", "text", "Expected signer fingerprint"}, {"require-signed", "bool", "Require signed metadata"},
	},
	"register": append(customerCompletionOptions(), completionOption{"confirm", "bool", "Confirm own browser enrollment"}, completionOption{"confirmation-file", "file", "Private confirmation file"}),
	"login":    customerCompletionOptions(),
	"unlist":   lifecycleCompletionOptions(false),
	"delete":   lifecycleCompletionOptions(true),
	"json":     {{"no-provenance", "bool", "Disable automatic provenance"}},
}

func customerCompletionOptions() []completionOption {
	return []completionOption{{"address", "text", "Explicit customer SSH host:port"}, {"known-hosts", "file", "Pinned SSH host keys"}, {"identity", "file", "Client SSH identity"}, {"server", "text", "Expected HTTPS account origin"}}
}

func lifecycleCompletionOptions(deletion bool) []completionOption {
	options := []completionOption{
		{"in", "dir", "Repository directory"}, {"package", "text", "Exact package name"},
		{"ver", "text", "Exact package version"}, {"arch", "arch", "Exact architecture"},
		{"gpg", "text", "Signing key fingerprint"}, {"codename", "text", "Distribution codename"},
		{"components", "text", "Repository component"},
	}
	if deletion {
		options = append(options, completionOption{"yes", "bool", "Confirm deletion"})
	}
	return options
}

func completionChoices(kind string) string {
	switch kind {
	case "arch":
		return "all amd64 arm64 armhf i386 riscv64"
	case "yesno":
		return "yes no"
	case "multiarch":
		return "no same foreign allowed"
	case "shell":
		return "bash zsh fish"
	}
	return ""
}

func commandCompletionOptions(command string) []completionOption {
	return append(append([]completionOption{}, completionOptions[command]...), completionOption{"help", "bool", "Show help"})
}

func writeCompletion(output io.Writer, shell string) error {
	var script strings.Builder
	switch shell {
	case "bash":
		writeBashCompletion(&script)
	case "zsh":
		writeZshCompletion(&script)
	case "fish":
		writeFishCompletion(&script)
	default:
		return fmt.Errorf("unsupported shell %q; choose bash, zsh, or fish", shell)
	}
	_, err := io.WriteString(output, script.String())
	return err
}

func completionSpellings(option completionOption) (string, string) {
	if option.name == "help" {
		return "-h", "--help"
	}
	return "-" + option.name, "--" + option.name
}

func completionCommandNames() string {
	names := []string{"help"}
	for _, command := range publicCommands {
		names = append(names, command.Name)
	}
	return strings.Join(names, " ")
}

func writeBashCompletion(w io.Writer) {
	fmt.Fprint(w, `# Bash completion for tpa; loaded automatically by bash-completion.
_tpa() {
    local cur prev words cword command options kind option prefix= i
    _init_completion -n = || return
    command=${words[1]}
    if (( cword == 1 )); then
`)
	fmt.Fprintf(w, "        COMPREPLY=( $(compgen -W '%s -h --help -version --version' -- \"$cur\") ); return\n", completionCommandNames())
	fmt.Fprint(w, "    fi\n    if [[ $command == stage && $cword == 2 ]]; then\n        COMPREPLY=( $(compgen -W 'create upload inspect cancel' -- \"$cur\") ); return\n    fi\n    case \"$command\" in\n")
	for _, command := range publicCommands {
		var names []string
		for _, option := range commandCompletionOptions(command.Name) {
			short, long := completionSpellings(option)
			names = append(names, short, long)
		}
		fmt.Fprintf(w, "        %s) options='%s' ;;\n", command.Name, strings.Join(names, " "))
	}
	fmt.Fprint(w, `        *) return ;;
    esac
    option=$prev
    if [[ $cur == -*=* ]]; then
        option=${cur%%=*}
        prefix=$option=
        cur=${cur#*=}
    fi
    case "$command:${option#-}" in
`)
	for _, command := range publicCommands {
		for _, option := range commandCompletionOptions(command.Name) {
			fmt.Fprintf(w, "        %s:%s|%s:-%s) kind=%s ;;\n", command.Name, option.name, command.Name, option.name, option.kind)
		}
	}
	fmt.Fprint(w, `    esac
    if [[ $kind == bool && -n $prefix ]]; then
        COMPREPLY=( $(compgen -W 'true false' -- "$cur") )
    elif [[ -n $kind && $kind != bool ]]; then
        case "$kind" in
            dir) _filedir -d ;;
            file) _filedir ;;
`)
	for _, kind := range []string{"arch", "yesno", "multiarch", "shell"} {
		fmt.Fprintf(w, "            %s) COMPREPLY=( $(compgen -W '%s' -- \"$cur\") ) ;;\n", kind, completionChoices(kind))
	}
	fmt.Fprint(w, `        esac
    elif [[ $cur == -* ]]; then
        COMPREPLY=( $(compgen -W "$options" -- "$cur") )
    else
        case "$command" in
            completion) COMPREPLY=( $(compgen -W 'bash zsh fish' -- "$cur") ) ;;
            pack|inspect|verify|check) _filedir ;;
        esac
    fi
    if [[ -n $prefix ]]; then
        for i in "${!COMPREPLY[@]}"; do COMPREPLY[i]=$prefix${COMPREPLY[i]}; done
    fi
}
complete -F _tpa tpa
`)
}

func writeZshCompletion(w io.Writer) {
	fmt.Fprint(w, "#compdef tpa\n# Installed in the system vendor completion directory.\n_tpa() {\n    local context state state_descr line\n    local -a words=( \"${words[@]}\" )\n    local CURRENT=$CURRENT\n    typeset -A opt_args\n    local -a commands\n    commands=(\n")
	for _, command := range publicCommands {
		fmt.Fprintf(w, "        '%s:%s'\n", command.Name, command.Description)
	}
	fmt.Fprint(w, `        'help:Show help'
    )
    if (( CURRENT == 2 )); then
        _describe 'command' commands
        _arguments '(-h --help)'{-h,--help}'[Show help]' '(-version --version)'{-version,--version}'[Show version]'
        return
    fi
    words=( "${words[@]:1}" )
    (( CURRENT-- ))
    case $words[1] in
`)
	for _, command := range publicCommands {
		fmt.Fprintf(w, "        %s)\n            _arguments -s", command.Name)
		for _, option := range commandCompletionOptions(command.Name) {
			suffix := ""
			if option.kind != "bool" {
				action := ""
				if choices := completionChoices(option.kind); choices != "" {
					action = "(" + choices + ")"
				} else if option.kind == "file" {
					action = "_files"
				} else if option.kind == "dir" {
					action = "_files -/"
				}
				suffix = ":value:" + action
			}
			short, long := completionSpellings(option)
			fmt.Fprintf(w, " '(%s %s)'{%s,%s}'[%s]%s'", short, long, short, long, option.description, suffix)
		}
		switch command.Name {
		case "completion":
			fmt.Fprint(w, " '1:shell:(bash zsh fish)'")
		case "stage":
			fmt.Fprint(w, " '1:operation:(create upload inspect cancel)'")
		case "sign":
			fmt.Fprint(w, " '1:repository SSH URL:'")
		case "pack", "inspect", "verify", "check":
			fmt.Fprint(w, " '1:path or URL:_files'")
		}
		fmt.Fprint(w, "\n            ;;\n")
	}
	fmt.Fprint(w, "    esac\n}\n_tpa \"$@\"\n")
}

func writeFishCompletion(w io.Writer) {
	fmt.Fprint(w, "# Fish completion for tpa; loaded from vendor_completions.d.\nfunction __tpa_positional_files\n    set -l tokens (commandline -opc)\n    switch $tokens[-1]\n        case")
	seen := map[string]bool{}
	for _, command := range publicCommands {
		for _, option := range completionOptions[command.Name] {
			if option.kind != "bool" && !seen[option.name] {
				seen[option.name] = true
				fmt.Fprintf(w, " -%s --%s", option.name, option.name)
			}
		}
	}
	fmt.Fprint(w, "\n            return 1\n    end\n    not string match -q -- '-*' (commandline -ct)\nend\ncomplete -c tpa -f\ncomplete -c tpa -n '__fish_use_subcommand' -s h -l help -d 'Show help'\ncomplete -c tpa -n '__fish_use_subcommand' -o version -l version -d 'Show version'\n")
	for _, command := range publicCommands {
		fmt.Fprintf(w, "complete -c tpa -n '__fish_use_subcommand' -a '%s' -d '%s'\n", command.Name, command.Description)
		for _, option := range commandCompletionOptions(command.Name) {
			fmt.Fprintf(w, "complete -c tpa -n '__fish_seen_subcommand_from %s'", command.Name)
			if option.name == "help" {
				fmt.Fprint(w, " -s h -l help")
			} else {
				fmt.Fprintf(w, " -o %s -l %s", option.name, option.name)
			}
			fmt.Fprintf(w, " -d '%s'", option.description)
			if option.kind == "file" {
				fmt.Fprint(w, " -r")
			} else if option.kind != "bool" {
				// Fish -r implicitly enables file suggestions. Non-file values
				// need -x (requires an argument and disables file suggestions).
				fmt.Fprint(w, " -x")
			}
			if choices := completionChoices(option.kind); choices != "" {
				fmt.Fprintf(w, " -a '%s'", choices)
			} else if option.kind == "file" {
				fmt.Fprint(w, " -F")
			} else if option.kind == "dir" {
				fmt.Fprint(w, " -a '(__fish_complete_directories)' ")
			}
			fmt.Fprintln(w)
		}
	}
	fmt.Fprint(w, `complete -c tpa -n '__fish_use_subcommand' -a help -d 'Show help'
complete -c tpa -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
complete -c tpa -n '__fish_seen_subcommand_from stage; and not __fish_seen_subcommand_from create upload inspect cancel' -a 'create upload inspect cancel'
complete -c tpa -n '__fish_seen_subcommand_from pack inspect verify check; and __tpa_positional_files' -F
`)
}
