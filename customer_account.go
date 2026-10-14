package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// Reuse the authoritative pinned-host client, session store and SSH protocol.
// These commands do not reinterpret generic ssh:// repository destinations.
func runCustomerAccount(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	operation := args[0]
	forward := []string{}
	confirmation := false
	for _, arg := range args[1:] {
		if operation == "register" && (arg == "--confirm" || arg == "-confirm") {
			if confirmation {
				fmt.Fprintln(stderr, "duplicate --confirm")
				return 2
			}
			confirmation = true
			continue
		}
		forward = append(forward, arg)
	}
	if confirmation {
		operation = "confirm"
	}
	binary, err := exec.LookPath("tparun")
	if err != nil {
		fmt.Fprintln(stderr, "Customer account commands require tparun with the customer-ssh client installed.")
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	child := exec.CommandContext(ctx, binary, append([]string{"customer-ssh", operation}, forward...)...)
	child.Stdin = stdin
	child.Stdout = stdout
	child.Stderr = stderr
	child.WaitDelay = 5 * time.Second
	if err = child.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if exit.ExitCode() > 0 {
				return exit.ExitCode()
			}
		}
		fmt.Fprintln(stderr, "Customer account client failed:", err)
		return 1
	}
	return 0
}
