// Package aptpackage serves as a high-performance helper for automating
// the generation and management of Debian package archives and repository structures.
// It abstracts complex packaging, indexing, and filesystem synchronization processes,
// enabling efficient, routing-based processing of repository builds at the filesystem level.
package aptpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Repo struct {
	Control Control
	Dist    string
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func Pack(cfg Config) error {
	files, err := os.ReadDir(cfg.InDir)
	must(err)
	pkgs := []Repo{}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".deb") {
			pkg, err := ParsePackage(cfg.InDir + "/" + file.Name())
			must(err)
			pkgs = append(pkgs, Repo{
				Control: pkg,
				Dist:    cfg.InDir + "/" + file.Name(),
			})
		}
	}

	err = os.MkdirAll(fmt.Sprintf("%s/dists/%s/%s", cfg.OutDir, cfg.Repo.Codename, cfg.Repo.Components), 0o755)
	must(err)
	err = os.MkdirAll(fmt.Sprintf("%s/pool/%s", cfg.OutDir, cfg.Repo.Components), 0o755)
	must(err)

	pkgMap := make(map[string][]Repo)
	initalMap := make(map[byte]int)
	for _, pkg := range pkgs {
		pkgMap[pkg.Control.Architecture] = append(pkgMap[pkg.Control.Architecture], pkg)
		initalMap[pkg.Control.Name[0]] = 0
	}

	releasePath := filepath.Join(cfg.OutDir, "dists", cfg.Repo.Codename, "Release")
	inReleasePath := filepath.Join(cfg.OutDir, "dists", cfg.Repo.Codename, "InRelease")
	releaseFile, err := os.Create(releasePath)
	must(err)
	archList := []string{}
	for arch := range pkgMap {
		archList = append(archList, arch)
	}

	_, err = fmt.Fprintf(releaseFile, "Origin: %s\n", "TPA-Repo")
	must(err)
	_, err = fmt.Fprintf(releaseFile, "Label: %s\n", "TPA-Repo")
	must(err)
	_, err = fmt.Fprintf(releaseFile, "Suite: %s\n", "stable")
	must(err)
	_, err = fmt.Fprintf(releaseFile, "Architectures: %s\n", strings.Join(archList, " "))
	must(err)
	_, err = fmt.Fprintf(releaseFile, "Components: %s\n", "main")
	must(err)
	_, err = fmt.Fprintf(releaseFile, "Codename: %s\n", "stable")
	must(err)
	_, err = fmt.Fprintf(releaseFile, "Date: %s\n", time.Now().UTC().Format(time.RFC1123Z))
	must(err)
	_, err = fmt.Fprintf(releaseFile, "Description: %s\n", "My Private TPA Repository")
	must(err)
	_, err = fmt.Fprintf(releaseFile, "SHA256:\n")
	must(err)

	for _, arch := range archList {
		archPath := filepath.Join(cfg.OutDir, "dists", cfg.Repo.Codename, cfg.Repo.Components, "binary-"+arch)
		packagesPath := filepath.Join(cfg.OutDir, "dists", cfg.Repo.Codename, cfg.Repo.Components, "binary-"+arch, "Packages")
		err := os.MkdirAll(archPath, 0o755)
		must(err)
		packagesFile, err := os.Create(packagesPath)
		must(err)

		for _, deb := range pkgMap[arch] {
			_, err = fmt.Fprintf(packagesFile, "Package: %s\n", deb.Control.Name)
			must(err)
			_, err = fmt.Fprintf(packagesFile, "Version: %s\n", deb.Control.Version)
			must(err)
			_, err = fmt.Fprintf(packagesFile, "Architecture: %s\n", deb.Control.Architecture)
			must(err)
			relPath := filepath.Join(
				"pool",
				cfg.Repo.Codename,
				string(deb.Control.Name[0]),
				deb.Control.Name,
				filepath.Base(deb.Control.Name+".deb"),
			)
			_, err = fmt.Fprintf(packagesFile, "Filename: %s\n", relPath)
			must(err)
			_, err = fmt.Fprintf(packagesFile, "Hash: SHA256\n")
			must(err)
			fileInfo, err := os.Stat(deb.Dist)
			must(err)
			_, err = fmt.Fprintf(packagesFile, "Size: %d\n", fileInfo.Size())
			must(err)

			hash, err := getHash(deb.Dist)
			must(err)
			_, err = fmt.Fprintf(packagesFile, "SHA256: %s\n\n", hash)
			must(err)
		}
		err = packagesFile.Close()
		must(err)

		cmd := exec.Command("gzip", "-fk", packagesPath)
		err = cmd.Run()
		must(err)

		{
			packagesHash, err := getHash(packagesPath)
			must(err)
			packageSize, err := os.Stat(packagesPath)
			must(err)
			_, err = fmt.Fprintf(releaseFile,
				" %s %d %s\n", packagesHash, packageSize.Size(),
				fmt.Sprintf("%s/"+"binary-%s/Packages", cfg.Repo.Components, arch),
			)
			must(err)
		}
		{
			packagesHash, err := getHash(packagesPath + ".gz")
			must(err)
			packageSize, err := os.Stat(packagesPath + ".gz")
			must(err)
			_, err = fmt.Fprintf(releaseFile,
				" %s %d %s\n",
				packagesHash,
				packageSize.Size(),
				fmt.Sprintf("%s/binary-%s/Packages.gz", cfg.Repo.Components, arch))
			must(err)
		}
	}
	err = releaseFile.Close()
	must(err)
	if cfg.GPG == "" {
		return nil
	}
	cmd := exec.Command(
		"gpg",
		"--batch", "--yes", "--clearsign",
		"-u", cfg.GPG,
		"-o", inReleasePath,
		releasePath,
	)
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	must(err)
	for initial := range initalMap {
		poolDir := filepath.Join(cfg.OutDir, "pool", cfg.Repo.Components, string(initial))

		err := os.MkdirAll(poolDir, 0o755)
		must(err)
	}
	for _, pkg := range pkgs {
		initial := string(pkg.Control.Name[0])
		poolDir := filepath.Join(cfg.OutDir, "pool", cfg.Repo.Codename, initial, pkg.Control.Name)
		err = os.MkdirAll(poolDir, 0o755)
		must(err)

		dest := filepath.Join(poolDir, filepath.Base(pkg.Control.Name+".deb"))
		err = CopyFile(pkg.Dist, dest)
		must(err)
	}
	return nil
}

func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

func getHash(path string) (string, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("error: %w", err)
	}
	rawbytes := sha256.Sum256(file)
	return hex.EncodeToString(rawbytes[:]), nil
}
