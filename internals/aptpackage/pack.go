package aptpackage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxPackWorkers = 32

type repositoryPackage struct {
	Control       Control
	ControlStanza []byte
	Dist          string
	Size          int64
	SHA256        string
}

// Pack creates and verifies repository metadata and copies packages into the
// pool. Pool population is deliberately independent of signing: unsigned
// repositories are useful for local testing and must still be complete.
func Pack(cfg Config) error {
	if err := buildRepository(cfg); err != nil {
		return err
	}
	return verifyRepository(cfg)
}

// buildRepository materializes a repository at cfg.OutDir. AtomicPack uses it
// directly so it can set publication permissions before final verification.
func buildRepository(cfg Config) error {
	workers, err := packWorkerCount(cfg.Workers)
	if err != nil {
		return err
	}
	enumerationStage := startStage(stageDirectoryEnumeration)
	entries, err := os.ReadDir(cfg.InDir)
	enumerationStage()
	if err != nil {
		return fmt.Errorf("read package directory: %w", err)
	}

	pkgs, err := inspectAndDeduplicate(entries, cfg.InDir, workers)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no .deb packages found in %s", cfg.InDir)
	}
	sortStage := startStage(stageSortAndGroup)
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Control.Architecture != pkgs[j].Control.Architecture {
			return pkgs[i].Control.Architecture < pkgs[j].Control.Architecture
		}
		if pkgs[i].Control.Name != pkgs[j].Control.Name {
			return pkgs[i].Control.Name < pkgs[j].Control.Name
		}
		if pkgs[i].Control.Version != pkgs[j].Control.Version {
			return pkgs[i].Control.Version < pkgs[j].Control.Version
		}
		return filepath.Base(pkgs[i].Dist) < filepath.Base(pkgs[j].Dist)
	})
	sortStage()

	component := cfg.Repo.Components
	if component == "" {
		component = "main"
	}
	codename := cfg.Repo.Codename
	if codename == "" {
		codename = "stable"
	}
	distDir := filepath.Join(cfg.OutDir, "dists", codename)
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("create distribution directory: %w", err)
	}

	// Copy first, so a successful return always leaves a usable pool.
	if err := runPackageJobs(len(pkgs), workers, func(index int) error {
		pkg := pkgs[index]
		poolDir := filepath.Join(cfg.OutDir, "pool", component,
			string(pkg.Control.Name[0]), pkg.Control.Name)
		poolStage := startStage(stagePoolMkdir)
		if err := os.MkdirAll(poolDir, 0o755); err != nil {
			poolStage()
			return fmt.Errorf("create pool directory for %s: %w", filepath.Base(pkg.Dist), err)
		}
		poolStage()
		dest := filepath.Join(poolDir, filepath.Base(pkg.Dist))
		copyStage := startStage(stagePoolCopy)
		hash, size, err := copyFileAndHash(pkg.Dist, dest)
		copyStage()
		if err != nil {
			return fmt.Errorf("copy %s: %w", pkg.Dist, err)
		}
		pkgs[index].SHA256 = hash
		pkgs[index].Size = size
		return nil
	}); err != nil {
		return err
	}

	groupStage := startStage(stageGroupArchitectures)
	byArch := make(map[string][]repositoryPackage)
	for _, pkg := range pkgs {
		byArch[pkg.Control.Architecture] = append(byArch[pkg.Control.Architecture], pkg)
	}
	architectures := make([]string, 0, len(byArch))
	for arch := range byArch {
		architectures = append(architectures, arch)
	}
	sort.Strings(architectures)
	groupStage()

	releasePath := filepath.Join(distDir, "Release")
	release, err := os.Create(releasePath)
	if err != nil {
		return fmt.Errorf("create Release: %w", err)
	}
	closeRelease := func() error { return release.Close() }
	origin, label, suite := cfg.Repo.Origin, cfg.Repo.Label, cfg.Repo.Suite
	if origin == "" {
		origin = "TPA-Repo"
	}
	if label == "" {
		label = origin
	}
	if suite == "" {
		suite = codename
	}
	description := cfg.Repo.Description
	if description == "" {
		description = "TPA package repository"
	}
	releaseWriteStage := startStage(stageReleaseWrite)
	_, err = fmt.Fprintf(release, "Origin: %s\nLabel: %s\nSuite: %s\nArchitectures: %s\nComponents: %s\nCodename: %s\nDate: %s\nDescription: %s\nSHA256:\n",
		origin, label, suite, strings.Join(architectures, " "), component, codename,
		time.Now().UTC().Format(time.RFC1123Z), description)
	releaseWriteStage()
	if err != nil {
		_ = closeRelease()
		return fmt.Errorf("write Release: %w", err)
	}

	for _, arch := range architectures {
		binaryDir := filepath.Join(distDir, component, "binary-"+arch)
		if err := os.MkdirAll(binaryDir, 0o755); err != nil {
			_ = closeRelease()
			return fmt.Errorf("create metadata directory: %w", err)
		}
		packagesPath := filepath.Join(binaryDir, "Packages")
		packages, err := os.Create(packagesPath)
		if err != nil {
			_ = closeRelease()
			return fmt.Errorf("create Packages: %w", err)
		}
		packagesWriteStage := startStage(stagePackagesWrite)
		for _, pkg := range byArch[arch] {
			relPath := filepath.ToSlash(filepath.Join("pool", component, string(pkg.Control.Name[0]), pkg.Control.Name, filepath.Base(pkg.Dist)))
			if _, err = packages.Write(pkg.ControlStanza); err == nil {
				_, err = fmt.Fprintf(packages, "Filename: %s\nSize: %d\nSHA256: %s\n\n", relPath, pkg.Size, pkg.SHA256)
			}
			if err != nil {
				_ = packages.Close()
				_ = closeRelease()
				return fmt.Errorf("write Packages: %w", err)
			}
		}
		if err := packages.Close(); err != nil {
			packagesWriteStage()
			_ = closeRelease()
			return fmt.Errorf("close Packages: %w", err)
		}
		packagesWriteStage()
		compressionStage := startStage(stageCompression)
		if err := gzipFile(packagesPath); err != nil {
			compressionStage()
			_ = closeRelease()
			return err
		}
		compressionStage()
		for _, path := range []string{packagesPath, packagesPath + ".gz"} {
			releaseHashStage := startStage(stageReleaseIndexHash)
			hash, err := getHash(path)
			if err != nil {
				releaseHashStage()
				_ = closeRelease()
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				releaseHashStage()
				_ = closeRelease()
				return err
			}
			releaseHashStage()
			rel := filepath.ToSlash(filepath.Join(component, "binary-"+arch, filepath.Base(path)))
			releaseWriteStage = startStage(stageReleaseWrite)
			_, err = fmt.Fprintf(release, " %s %d %s\n", hash, info.Size(), rel)
			releaseWriteStage()
			if err != nil {
				_ = closeRelease()
				return fmt.Errorf("write Release hash: %w", err)
			}
		}
	}
	if err := closeRelease(); err != nil {
		return fmt.Errorf("close Release: %w", err)
	}
	if cfg.GPG != "" {
		keyLookupStage := startStage(stageSignKeyLookup)
		fingerprint, err := signingFingerprint(cfg.GPG)
		keyLookupStage()
		if err != nil {
			return err
		}
		inRelease := filepath.Join(distDir, "InRelease")
		cmd := exec.Command("gpg", "--batch", "--yes", "--clearsign", "-u", fingerprint, "-o", inRelease, releasePath)
		signStage := startStage(stageSignCommand)
		if output, err := cmd.CombinedOutput(); err != nil {
			signStage()
			return fmt.Errorf("sign Release: %w: %s", err, strings.TrimSpace(string(output)))
		}
		signStage()
	}
	return writeRepositoryBrowserFiles(cfg.OutDir, cfg.Repo)
}

// repositoryControlStanza preserves the control metadata emitted by dpkg-deb
// while removing fields whose values must be derived from the published file.
func repositoryControlStanza(raw []byte) ([]byte, error) {
	controlled := map[string]bool{"filename": true, "size": true, "sha256": true}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var out bytes.Buffer
	skip := false
	seenField := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if !seenField {
				return nil, fmt.Errorf("control continuation without a field")
			}
			if !skip {
				out.WriteString(line)
				out.WriteByte('\n')
			}
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("invalid control field %q", line)
		}
		seenField = true
		skip = controlled[strings.ToLower(line[:colon])]
		if !skip {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read control stanza: %w", err)
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("empty control stanza")
	}
	return out.Bytes(), nil
}

func gzipFile(path string) error {
	cmd := exec.Command("gzip", "-fk", "-n", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compress %s: %w: %s", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyFileAndHash(src, dst string) (string, int64, error) {
	source, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer source.Close()
	dest, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(dest, hash), source)
	closeErr := dest.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func filesEqual(first, second string) (bool, error) {
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false, err
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		return false, err
	}
	if firstInfo.Size() != secondInfo.Size() {
		return false, nil
	}
	firstFile, err := os.Open(first)
	if err != nil {
		return false, err
	}
	defer firstFile.Close()
	secondFile, err := os.Open(second)
	if err != nil {
		return false, err
	}
	defer secondFile.Close()
	firstBuffer := make([]byte, 64*1024)
	secondBuffer := make([]byte, 64*1024)
	for {
		firstN, firstErr := firstFile.Read(firstBuffer)
		secondN, secondErr := secondFile.Read(secondBuffer)
		if firstN != secondN || !bytes.Equal(firstBuffer[:firstN], secondBuffer[:secondN]) {
			return false, nil
		}
		if firstErr == io.EOF && secondErr == io.EOF {
			return true, nil
		}
		if firstErr != nil {
			return false, firstErr
		}
		if secondErr != nil {
			return false, secondErr
		}
	}
}

func getHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type packageJobResult struct {
	index int
	err   error
}

type orderedInspectionResult struct {
	packageData repositoryPackage
	skip        bool
	err         error
}

func packWorkerCount(requested int) (int, error) {
	if requested < 0 || requested > MaxPackWorkers {
		return 0, fmt.Errorf("package worker count must be between 0 and %d", MaxPackWorkers)
	}
	if requested == 0 {
		requested = runtime.GOMAXPROCS(0)
		if requested < 1 {
			requested = 1
		}
		if requested > MaxPackWorkers {
			requested = MaxPackWorkers
		}
	}
	return requested, nil
}

func inspectAndDeduplicate(entries []os.DirEntry, root string, workers int) ([]repositoryPackage, error) {
	packages := make([]repositoryPackage, 0)
	identities := make(map[string]repositoryPackage)
	err := runOrderedInspectionJobs(len(entries), workers, func(index int) (orderedInspectionResult, error) {
		entry := entries[index]
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".deb") {
			return orderedInspectionResult{skip: true}, nil
		}
		path := filepath.Join(root, entry.Name())
		inspectStage := startStage(stageInspect)
		rawControl, err := readPackageControl(path)
		inspectStage()
		if err != nil {
			return orderedInspectionResult{}, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		metadataStage := startStage(stageParseMetadata)
		control, err := ParseControl(rawControl)
		if err != nil {
			metadataStage()
			return orderedInspectionResult{}, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		stanza, err := repositoryControlStanza(rawControl)
		metadataStage()
		if err != nil {
			return orderedInspectionResult{}, fmt.Errorf("prepare metadata for %s: %w", entry.Name(), err)
		}
		statStage := startStage(stageStatSource)
		info, err := os.Stat(path)
		statStage()
		if err != nil {
			return orderedInspectionResult{}, fmt.Errorf("stat %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return orderedInspectionResult{}, fmt.Errorf("package is not a regular file: %s", entry.Name())
		}
		return orderedInspectionResult{packageData: repositoryPackage{
			Control: control, ControlStanza: stanza, Dist: path, Size: info.Size(),
		}}, nil
	}, func(_ int, result orderedInspectionResult) error {
		if result.skip {
			return nil
		}
		pkg := result.packageData
		identity := pkg.Control.Name + "\x00" + pkg.Control.Version + "\x00" + pkg.Control.Architecture
		if previous, ok := identities[identity]; ok {
			duplicateStage := startStage(stageDuplicateCompare)
			equal, err := filesEqual(previous.Dist, pkg.Dist)
			duplicateStage()
			if err != nil {
				return fmt.Errorf("compare duplicate package identity %s %s %s: %w", pkg.Control.Name, pkg.Control.Version, pkg.Control.Architecture, err)
			}
			if !equal {
				return fmt.Errorf("conflicting package identity %s %s %s in %s and %s", pkg.Control.Name, pkg.Control.Version, pkg.Control.Architecture, filepath.Base(previous.Dist), filepath.Base(pkg.Dist))
			}
			return nil
		}
		identities[identity] = pkg
		packages = append(packages, pkg)
		return nil
	})
	return packages, err
}

func runOrderedInspectionJobs(count, workers int, job func(index int) (orderedInspectionResult, error), consume func(index int, result orderedInspectionResult) error) error {
	if count == 0 {
		return nil
	}
	if workers < 1 || workers > MaxPackWorkers {
		return fmt.Errorf("invalid package worker count %d", workers)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan int, workers*2)
	type indexedResult struct {
		index  int
		result orderedInspectionResult
	}
	results := make(chan indexedResult, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					result, err := job(index)
					result.err = err
					results <- indexedResult{index: index, result: result}
					if err != nil {
						return
					}
				}
			}
		}()
	}
	go func() {
		group.Wait()
		close(results)
	}()

	pending := make(map[int]orderedInspectionResult, workers*2)
	next, submitted := 0, 0
	jobsClosed := false
	taskErrIndex := count
	var taskErr, consumeErr error
	dispatch := func() {
		if jobsClosed {
			return
		}
		if ctx.Err() != nil {
			close(jobs)
			jobsClosed = true
			return
		}
		windowEnd := next + workers*2
		for submitted < count && submitted < windowEnd {
			jobs <- submitted
			submitted++
		}
		if submitted == count {
			close(jobs)
			jobsClosed = true
		}
	}
	dispatch()
	for completed := range results {
		if completed.result.err != nil && completed.index < taskErrIndex {
			taskErrIndex = completed.index
			taskErr = completed.result.err
			cancel()
		}
		pending[completed.index] = completed.result
		for {
			result, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			if result.err != nil {
				if next < taskErrIndex {
					taskErrIndex = next
					taskErr = result.err
				}
				cancel()
			} else if next < taskErrIndex && consumeErr == nil {
				if err := consume(next, result); err != nil {
					consumeErr = err
					cancel()
				}
			}
			next++
		}
		if taskErr != nil || consumeErr != nil {
			cancel()
		}
		dispatch()
	}
	if !jobsClosed {
		close(jobs)
	}
	if consumeErr != nil {
		return consumeErr
	}
	if taskErr != nil {
		return taskErr
	}
	if next != count {
		return fmt.Errorf("package inspection canceled after %d of %d entries", next, count)
	}
	return nil
}

// runPackageJobs executes independent indexed tasks with bounded queues. The
// caller gathers task results by index, so worker completion order cannot
// affect repository ordering.
func runPackageJobs(count, workers int, job func(index int) error) error {
	if count == 0 {
		return nil
	}
	if workers < 1 || workers > MaxPackWorkers {
		return fmt.Errorf("invalid package worker count %d", workers)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan int, workers*2)
	results := make(chan packageJobResult, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					err := job(index)
					results <- packageJobResult{index: index, err: err}
					if err != nil {
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := 0; index < count; index++ {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case jobs <- index:
			}
		}
	}()
	go func() {
		group.Wait()
		close(results)
	}()

	completed := 0
	firstErrorIndex := count
	var firstError error
	for result := range results {
		completed++
		if result.err != nil {
			cancel()
		}
		if result.err != nil && result.index < firstErrorIndex {
			firstErrorIndex = result.index
			firstError = result.err
		}
	}
	if firstError != nil {
		return firstError
	}
	if completed != count {
		return fmt.Errorf("package work canceled after %d of %d jobs", completed, count)
	}
	return nil
}
