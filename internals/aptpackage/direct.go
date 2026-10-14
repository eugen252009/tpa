package aptpackage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DirectOptions bounds archive-building scratch concurrency, not total Go RSS.
// Retained definitions/metadata have a separate 64 MiB serialized input bound.
// A small budget reduces workers; a record that cannot fit one worker fails
// before any candidate is written. There is no temporary-package-dir fallback.
type DirectOptions struct {
	Workers     int
	MemoryBytes int64
	Epoch       int64 // explicit reproducible Unix time (default 0); signing time is separate
}

type directRecord struct {
	cfg         Config
	control     []byte
	fingerprint [32]byte
}

func normalizeDirect(cfg Config, epoch int64) (directRecord, error) {
	if epoch < 0 || epoch > 1<<32-1 {
		return directRecord{}, fmt.Errorf("epoch must be between 0 and 4294967295")
	}
	if cfg.InDir != "" || cfg.OutDir != "" || cfg.GPG != "" || cfg.Repo != (RepoConfig{}) {
		return directRecord{}, fmt.Errorf("direct definitions only accept control, scripts and provenance; specify repository/signing/output options on compose, not per package")
	}
	var control string
	var err error
	if cfg.Provenance != nil && !*cfg.Provenance {
		control, err = cfg.Control.Render()
	} else {
		control, err = cfg.Control.RenderWithProvenance(time.Unix(epoch, 0).UTC())
	}
	if err != nil {
		return directRecord{}, err
	}
	scripts := scriptBodies(cfg.Scripts)
	for i, body := range scripts {
		if body != "" && (!strings.HasPrefix(body, "#!") || strings.ContainsRune(body, 0)) {
			return directRecord{}, fmt.Errorf("%s: direct maintainer scripts require an explicit #! interpreter and no NUL bytes; see https://www.debian.org/doc/debian-policy/ch-maintainerscripts.html", scriptNames[i])
		}
	}
	data, err := json.Marshal(struct {
		Control string
		Scripts []string
	}{control, scripts})
	if err != nil {
		return directRecord{}, err
	}
	if len(data) > MaxDefinitionBytes {
		return directRecord{}, fmt.Errorf("normalized definition exceeds %d bytes", MaxDefinitionBytes)
	}
	return directRecord{cfg: cfg, control: []byte(control), fingerprint: sha256.Sum256(data)}, nil
}

var scriptNames = []string{"preinst", "postinst", "prerm", "postrm"}

func scriptBodies(s MaintainerScripts) []string {
	return []string{s.PreInst, s.PostInst, s.PreRm, s.PostRm}
}

// WriteMetadataDeb writes a reproducible Debian 2.0 archive directly to w.
// Only the control gzip and empty data gzip are buffered, never a package tree.
func WriteMetadataDeb(w io.Writer, cfg Config, epoch int64) error {
	record, err := normalizeDirect(cfg, epoch)
	if err != nil {
		return err
	}
	return writeMetadataDeb(w, record, epoch)
}
func writeMetadataDeb(w io.Writer, record directRecord, epoch int64) error {
	var control bytes.Buffer
	gz := gzip.NewWriter(&control)
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	add := func(name string, body []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: "./" + name, Mode: mode, Size: int64(len(body)), Uid: 0, Gid: 0, ModTime: time.Unix(epoch, 0), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}
	if err := add("control", record.control, 0644); err != nil {
		return err
	}
	for i, body := range scriptBodies(record.cfg.Scripts) {
		if body != "" {
			if err := add(scriptNames[i], []byte(body), 0755); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	// A valid data tar is required even for a payload-free metapackage.
	var data bytes.Buffer
	dgz := gzip.NewWriter(&data)
	dgz.Header.OS = 255
	if err := tar.NewWriter(dgz).Close(); err != nil {
		return err
	}
	if err := dgz.Close(); err != nil {
		return err
	}
	if err := writeArchiveBytes(w, []byte("!<arch>\n")); err != nil {
		return err
	}
	for _, member := range []struct {
		name string
		data []byte
	}{{"debian-binary", []byte("2.0\n")}, {"control.tar.gz", control.Bytes()}, {"data.tar.gz", data.Bytes()}} {
		header := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8o%-10d`\n", member.name+"/", epoch, 0, 0, 0644, len(member.data))
		if len(header) != 60 {
			return fmt.Errorf("ar header overflow")
		}
		if err := writeArchiveBytes(w, []byte(header)); err != nil {
			return err
		}
		if err := writeArchiveBytes(w, member.data); err != nil {
			return err
		}
		if len(member.data)%2 != 0 {
			if err := writeArchiveBytes(w, []byte("\n")); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeArchiveBytes(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Compose builds a new local repository only. It never replaces an existing
// destination. Every candidate is independently verified before exclusive
// activation using the existing publication primitive. Published=true errors
// from a post-activation parent sync retain their existing typed semantics.
func Compose(ctx context.Context, cfg Config, input string, opts DirectOptions) error {
	definitions, err := ReadDefinitions(input)
	if err != nil {
		return err
	}
	return ComposeDefinitions(ctx, cfg, definitions, opts)
}
func ComposeDefinitions(ctx context.Context, cfg Config, definitions []Config, opts DirectOptions) error {
	if !repositoryInitializationSupported() {
		return fmt.Errorf("safe new-destination activation unsupported on this platform")
	}
	if cfg.OutDir == "" {
		return fmt.Errorf("output path required")
	}
	workers, err := packWorkerCount(opts.Workers)
	if err != nil {
		return err
	}
	if opts.MemoryBytes == 0 {
		opts.MemoryBytes = 64 << 20
	}
	if opts.MemoryBytes < 0 {
		return fmt.Errorf("memory budget must be positive")
	}
	if len(definitions) == 0 || len(definitions) > maxRepositoryPackageCount {
		return fmt.Errorf("expected 1..%d definitions", maxRepositoryPackageCount)
	}
	records := make([]directRecord, 0, len(definitions))
	seen := make(map[PackageIdentity][32]byte)
	total, maxRecord := 0, 0
	for i, definition := range definitions {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, err := normalizeDirect(definition, opts.Epoch)
		if err != nil {
			return fmt.Errorf("definition %d: %w", i+1, err)
		}
		size := len(record.control)
		for _, s := range scriptBodies(record.cfg.Scripts) {
			size += len(s)
		}
		total += size
		if total > MaxDefinitionsBytes {
			return fmt.Errorf("normalized definitions exceed memory/input bound")
		}
		if size > maxRecord {
			maxRecord = size
		}
		id := packageIdentity(definition.Control)
		if old, ok := seen[id]; ok {
			if old != record.fingerprint {
				return fmt.Errorf("conflicting duplicate package identity %s; different content requires a new version", id)
			}
			continue
		}
		seen[id] = record.fingerprint
		records = append(records, record)
	}
	perWorker := int64(8<<20) + int64(4*maxRecord)
	if opts.MemoryBytes < perWorker {
		return fmt.Errorf("memory budget %d cannot fit one archive worker (requires %d); increase -memory-mib", opts.MemoryBytes, perWorker)
	}
	if n := int(opts.MemoryBytes / perWorker); workers > n {
		workers = n
	}
	if workers > len(records) {
		workers = len(records)
	}
	live, err := filepath.Abs(cfg.OutDir)
	if err != nil {
		return err
	}
	if live == string(filepath.Separator) {
		return fmt.Errorf("cannot compose into filesystem root")
	}
	if err := os.MkdirAll(filepath.Dir(live), 0755); err != nil {
		return err
	}
	return withRepositoryLock(live, func() (resultErr error) {
		if _, err := os.Lstat(live); err == nil {
			return fmt.Errorf("compose destination already exists; replacement is not supported")
		} else if !os.IsNotExist(err) {
			return err
		}
		staging, err := os.MkdirTemp(filepath.Dir(live), "."+filepath.Base(live)+".tpa-compose-")
		if err != nil {
			return err
		}
		defer func() {
			if err := os.RemoveAll(staging); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("cleanup compose candidate %s: %w", staging, err))
			}
		}()
		cfg.OutDir = staging
		cfg.Workers = workers
		cfg.releaseDate = time.Unix(opts.Epoch, 0).UTC().Format(time.RFC1123Z)
		component := cfg.Repo.Components
		if component == "" {
			component = "main"
		}
		if !validRepositoryNameSegment(component) {
			return fmt.Errorf("invalid repository component %q", component)
		}
		packages := make([]repositoryPackage, len(records))
		failures := make([]error, workers)
		workCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		var group sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			start, end := len(records)*worker/workers, len(records)*(worker+1)/workers
			group.Go(func() {
				for i := start; i < end; i++ {
					if err := workCtx.Err(); err != nil {
						failures[worker] = err
						return
					}
					record := records[i]
					c := record.cfg.Control
					// Epoch colons are safe in control metadata but not in portable pool basenames.
					filename := c.Name + "_" + strings.ReplaceAll(c.Version, ":", "_") + "_" + c.Architecture + ".deb"
					path := filepath.Join(staging, "pool", component, c.Name[:1], c.Name, filename)
					if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
						failures[worker] = err
						cancel()
						return
					}
					f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
					if err != nil {
						failures[worker] = err
						cancel()
						return
					}
					hash := sha256.New()
					counter := &countWriter{w: io.MultiWriter(f, hash)}
					buildErr := writeMetadataDeb(counter, record, opts.Epoch)
					closeErr := f.Close()
					if err := errors.Join(buildErr, closeErr); err != nil {
						failures[worker] = err
						cancel()
						return
					}
					stanza, err := repositoryControlStanza(record.control)
					if err != nil {
						failures[worker] = err
						cancel()
						return
					}
					packages[i] = repositoryPackage{Control: c, ControlStanza: stanza, Dist: path, Size: counter.n, SHA256: hex.EncodeToString(hash.Sum(nil))}
				}
			})
		}
		group.Wait()
		if err := errors.Join(failures...); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := buildRepositoryRecords(cfg, packages, true); err != nil {
			return err
		}
		if err := prepareRepositoryPermissions(staging); err != nil {
			return err
		}
		if err := VerifyTPARepositoryTree(cfg); err != nil {
			return fmt.Errorf("independent candidate verification: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return publishInitializedDirectory(staging, live)
	})
}
