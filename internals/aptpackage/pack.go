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

func (p *PackPackage) Pack() error {
	if p.Flat {
		return fmt.Errorf("todo")
	}

	files, err := os.ReadDir(p.Context.InDir)
	if err != nil {
		return err
	}
	pkgs := []Repo{}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".deb") {
			pkg, err := ParsePackage(p.Context.InDir + "/" + file.Name())
			if err != nil {
				return err
			}
			pkgs = append(pkgs, Repo{
				Control: pkg,
				Dist:    p.Context.InDir + "/" + file.Name(),
			})
		}
	}

	os.MkdirAll(p.Context.OutDir+"/dists/stable/main", 0o755)
	os.MkdirAll(p.Context.OutDir+"/pool/main", 0o755)

	pkgMap := make(map[string][]Repo)
	initalMap := make(map[byte]int)
	for _, pkg := range pkgs {
		pkgMap[pkg.Control.Architecture] = append(pkgMap[pkg.Control.Architecture], pkg)
		initalMap[pkg.Control.Package[0]] = 0
	}

	releasePath := filepath.Join(p.Context.OutDir, "dists", "stable", "Release")
	inReleasePath := filepath.Join(p.Context.OutDir, "dists", "stable", "InRelease")
	releaseFile, err := os.Create(releasePath)
	if err != nil {
		return err
	}
	{
		archList := []string{}
		for arch := range pkgMap {
			archList = append(archList, arch)
		}

		fmt.Fprintf(releaseFile, "Origin: %s\n", "TPA-Repo")
		fmt.Fprintf(releaseFile, "Label: %s\n", "TPA-Repo")
		fmt.Fprintf(releaseFile, "Suite: %s\n", "stable")
		fmt.Fprintf(releaseFile, "Architectures: %s\n", strings.Join(archList, " "))
		fmt.Fprintf(releaseFile, "Components: %s\n", "main")
		fmt.Fprintf(releaseFile, "Codename: %s\n", "stable")
		fmt.Fprintf(releaseFile, "Date: %s\n", time.Now().UTC().Format(time.RFC1123Z))
		fmt.Println(time.Now().Format(time.RFC1123Z))
		fmt.Fprintf(releaseFile, "Description: %s\n", "My Private TPA Repository")
		fmt.Fprintf(releaseFile, "SHA256:\n")

		for _, arch := range archList {
			archPath := filepath.Join(p.Context.OutDir, "dists", "stable", "main", "binary-"+arch)
			packagesPath := filepath.Join(p.Context.OutDir, "dists", "stable", "main", "binary-"+arch, "Packages")
			if err := os.MkdirAll(archPath, 0o755); err != nil {
				return err
			}
			packagesFile, err := os.Create(packagesPath)
			if err != nil {
				return err
			}

			for _, deb := range pkgMap[arch] {
				fmt.Fprintf(packagesFile, "Package: %s\n", deb.Control.Package)
				fmt.Fprintf(packagesFile, "Version: %s\n", deb.Control.Version)
				fmt.Fprintf(packagesFile, "Architecture: %s\n", deb.Control.Architecture)
				relPath := filepath.Join("pool", "main", string(deb.Control.Package[0]), deb.Control.Package, filepath.Base(deb.Dist))
				fmt.Fprintf(packagesFile, "Filename: %s\n", relPath)
				fmt.Fprintf(packagesFile, "Hash: SHA256\n")

				fileInfo, err := os.Stat(deb.Dist)
				if err != nil {
					return err
				}
				fmt.Fprintf(packagesFile, "Size: %d\n", fileInfo.Size())

				hash, err := getHash(deb.Dist)
				if err != nil {
					return err
				}
				fmt.Fprintf(packagesFile, "SHA256: %s\n\n", hash)
			}
			packagesFile.Close()
			{
				cmd := exec.Command("gzip", "-fk", packagesPath)
				err = cmd.Run()
				if err != nil {
					return err
				}
			}
			{
				packagesHash, err := getHash(packagesPath)
				if err != nil {
					return err
				}
				packageSize, err := os.Stat(packagesPath)
				if err != nil {
					return err
				}
				fmt.Fprintf(releaseFile, " %s %d %s\n", packagesHash, packageSize.Size(), "main/"+"binary-"+arch+"/Packages")
			}
			{
				packagesHash, err := getHash(packagesPath + ".gz")
				if err != nil {
					return err
				}
				packageSize, err := os.Stat(packagesPath + ".gz")
				if err != nil {
					return err
				}
				fmt.Fprintf(releaseFile, " %s %d %s\n", packagesHash, packageSize.Size(), "main/"+"binary-"+arch+"/Packages.gz")
			}
		}
		releaseFile.Close()
		cmd := exec.Command(
			"gpg",
			"--batch", "--yes", "--clearsign",
			"-u", p.GPG,
			"-o", inReleasePath,
			releasePath,
		)
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return err
		}
		// err = os.Remove(releasePath)
		// if err != nil {
		// 	return err
		// }
		fmt.Println("DONE!")
	}
	{
		for initial := range initalMap {
			poolDir := filepath.Join(p.Context.OutDir, "pool", "main", string(initial))

			if err := os.MkdirAll(poolDir, 0o755); err != nil {
				return err
			}
		}
		for _, pkg := range pkgs {
			initial := string(pkg.Control.Package[0])
			poolDir := filepath.Join(p.Context.OutDir, "pool", "main", initial, pkg.Control.Package)
			os.MkdirAll(poolDir, 0o755)

			dest := filepath.Join(poolDir, filepath.Base(pkg.Dist))
			CopyFile(pkg.Dist, dest)
		}
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

func getHashToFile(path string) string {
	return ""
}
