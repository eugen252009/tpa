//go:build !linux

package main

func stdinIsTerminal() bool { return false }
