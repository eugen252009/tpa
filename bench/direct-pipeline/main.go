// Command direct-pipeline is a local benchmark harness, not a publication tool.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "direct", "direct or existing")
	input := flag.String("in", "", "JSONL corpus")
	output := flag.String("out", "", "Repository destination")
	workers := flag.Int("workers", 8, "Contiguous workers")
	flag.Parse()
	if *workers < 1 || *workers > 32 {
		return fmt.Errorf("invalid worker count")
	}
	// Suppress the existing builder's per-dpkg progress, not its errors.
	original := os.Stdout
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	os.Stdout = null
	defer func() { os.Stdout = original }()
	defs, err := aptpackage.ReadDefinitions(*input)
	if err != nil {
		return err
	}
	if *mode == "direct" {
		err = aptpackage.ComposeDefinitions(context.Background(), aptpackage.Config{OutDir: *output}, defs, aptpackage.DirectOptions{Workers: *workers, MemoryBytes: 128 << 20})
	} else if *mode == "existing" {
		temp, err := os.MkdirTemp(filepath.Dir(*output), "existing-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		debs := filepath.Join(temp, "debs")
		if err := os.Mkdir(debs, 0755); err != nil {
			return err
		}
		failures := make([]error, *workers)
		var group sync.WaitGroup
		for worker := 0; worker < *workers; worker++ {
			start, end := len(defs)*worker / *workers, len(defs)*(worker+1) / *workers
			group.Go(func() {
				for i := start; i < end; i++ {
					cfg := defs[i]
					tree := filepath.Join(temp, fmt.Sprintf("package-%d", i))
					cfg.OutDir = tree
					if err := aptpackage.InitPackage(cfg); err != nil {
						failures[worker] = err
						return
					}
					cfg.InDir = tree
					cfg.OutDir = filepath.Join(debs, cfg.Control.Name+"_"+cfg.Control.Version+"_"+cfg.Control.Architecture+".deb")
					if err := aptpackage.Build(cfg); err != nil {
						failures[worker] = err
						return
					}
					if err := os.RemoveAll(tree); err != nil {
						failures[worker] = err
						return
					}
				}
			})
		}
		group.Wait()
		for _, err := range failures {
			if err != nil {
				return err
			}
		}
		err = aptpackage.Pack(aptpackage.Config{InDir: debs, OutDir: *output, Workers: *workers})
	} else {
		return fmt.Errorf("unknown mode")
	}
	if err != nil {
		return err
	}
	var size int64
	files := 0
	err = filepath.Walk(*output, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files++
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(original).Encode(map[string]any{"mode": *mode, "packages": len(defs), "output_bytes": size, "output_files": files, "package_trees": map[bool]int{true: len(defs), false: 0}[*mode == "existing"], "dpkg_deb_processes": map[bool]int{true: len(defs), false: 0}[*mode == "existing"]})
}
