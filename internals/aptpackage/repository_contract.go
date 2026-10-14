package aptpackage

import (
	"fmt"
	"github.com/eugen252009/tpa/pkg/debian"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RepositoryFormatName       = "tpa-apt-repository"
	RepositoryFormatVersion    = 1
	RepositoryBrowserFormat    = "tpa-repository-index"
	RepositoryBrowserVersion   = 1
	RepositoryCapabilitiesName = "tpa-capabilities"
	RepositoryCapabilitiesV1   = 1
)

func validateReleaseField(name, value string) error {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("invalid Release field %s", name)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return fmt.Errorf("invalid control character in Release field %s", name)
		}
	}
	return nil
}

func validRepositoryNameSegment(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if index == 0 {
			if !valid {
				return false
			}
		} else if !valid && character != '+' && character != '.' && character != '-' {
			return false
		}
	}
	return true
}

func validRepositoryArchitecture(value string) bool {
	if value == "" || value == "source" {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if index == 0 {
			if !valid {
				return false
			}
		} else if !valid && character != '-' {
			return false
		}
	}
	return true
}

func validRepositoryPackageName(value string) bool {
	return debian.ValidPackageName(value)
}

func validRepositoryArtifactBasename(value string) bool {
	if !strings.HasSuffix(value, ".deb") || value == ".deb" {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if index == 0 {
			if !valid {
				return false
			}
		} else if !valid && character != '+' && character != '.' && character != '_' && character != '~' && character != '-' && character != ':' && character != '%' {
			return false
		}
	}
	return true
}

func validateEmptyRepository(cfg Config, architectures []string) error {
	if strings.TrimSpace(cfg.Repo.Suite) == "" {
		return fmt.Errorf("empty repository requires an explicit suite")
	}
	if cfg.Repo.Codename == "" || !validRepositoryNameSegment(cfg.Repo.Codename) {
		return fmt.Errorf("empty repository requires a safe explicit codename")
	}
	if cfg.Repo.Components == "" || !validRepositoryNameSegment(cfg.Repo.Components) {
		return fmt.Errorf("empty repository requires one safe explicit component")
	}
	if len(architectures) == 0 {
		return fmt.Errorf("empty repository requires at least one architecture")
	}
	seen := make(map[string]bool, len(architectures))
	for _, architecture := range architectures {
		if !validRepositoryArchitecture(architecture) {
			return fmt.Errorf("invalid repository architecture %q", architecture)
		}
		if seen[architecture] {
			return fmt.Errorf("duplicate repository architecture %q", architecture)
		}
		seen[architecture] = true
	}
	return nil
}

func repositoryReleaseDate() (string, error) {
	if value, ok := os.LookupEnv("SOURCE_DATE_EPOCH"); ok {
		epoch, err := strconv.ParseInt(value, 10, 64)
		if err != nil || epoch < 0 {
			return "", fmt.Errorf("invalid SOURCE_DATE_EPOCH %q", value)
		}
		instant := time.Unix(epoch, 0).UTC()
		if instant.Year() < 1 || instant.Year() > 9999 {
			return "", fmt.Errorf("SOURCE_DATE_EPOCH is outside the supported Release date range")
		}
		return instant.Format(time.RFC1123Z), nil
	}
	return time.Now().UTC().Format(time.RFC1123Z), nil
}
