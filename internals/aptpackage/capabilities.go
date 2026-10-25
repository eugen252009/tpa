package aptpackage

import (
	"runtime"

	"github.com/eugen252009/tpa/internal/version"
)

// Capabilities describes the stable machine-readable integration surface.
type Capabilities struct {
	Format           string                    `json:"format"`
	Version          int                       `json:"version"`
	TPAVersion       string                    `json:"tpa_version"`
	Platform         CapabilityPlatform        `json:"platform"`
	RepositoryFormat CapabilityRepository      `json:"repository_format"`
	Operations       []string                  `json:"operations"`
	Readers          []string                  `json:"readers"`
	Signing          CapabilitySigning         `json:"signing"`
	Publication      CapabilityPublication     `json:"publication"`
	Transport        CapabilityTransport       `json:"transport"`
	OutputBackends   []CapabilityOutputBackend `json:"output_backends"`
}

type CapabilityPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type CapabilityRepository struct {
	Name                            string `json:"name"`
	Version                         int    `json:"version"`
	BrowserIndex                    string `json:"browser_index"`
	BrowserIndexVersion             int    `json:"browser_index_version"`
	BrowserSidecarsPaired           bool   `json:"browser_sidecars_paired"`
	BrowserSidecarsRequired         bool   `json:"browser_sidecars_required"`
	BrowserSidecarsCoveredByRelease bool   `json:"browser_sidecars_covered_by_release"`
	EmptyInitialization             bool   `json:"empty_initialization"`
}

type CapabilitySigning struct {
	GPGInRelease bool `json:"gpg_inrelease"`
	ManagedKeys  bool `json:"managed_keys"`
}

type CapabilityPublication struct {
	AtomicReplaceLinuxRenameExchange bool   `json:"atomic_replace_linux_rename_exchange"`
	HostedManagedPublication         bool   `json:"hosted_managed_publication"`
	ClientSignedSSH                  bool   `json:"client_signed_ssh"`
	ClientSignedSSHProtocol          string `json:"client_signed_ssh_protocol"`
	ClientSignedSSHProtocolVersion   int    `json:"client_signed_ssh_protocol_version"`
	ClientSignedSSHChunkedCAS        bool   `json:"client_signed_ssh_chunked_cas"`
}

type CapabilityTransport struct {
	InboundSSHUpload    bool `json:"inbound_ssh_upload"`
	AuthenticatedUpload bool `json:"authenticated_upload"`
}

type CapabilityOutputBackend struct {
	Name                           string `json:"name"`
	RequiresOpenSSHClient          bool   `json:"requires_openssh_client"`
	StrictHostKeyChecking          bool   `json:"strict_host_key_checking"`
	NewDestinationOnly             bool   `json:"new_destination_only"`
	ExistingDestinationReplacement bool   `json:"existing_destination_replacement"`
	TPAWriterLock                  bool   `json:"tpa_writer_lock"`
	AtomicActivationGuarantee      string `json:"atomic_activation_guarantee"`
}

// GetCapabilities returns schema-v1 feature identifiers. The arrays are kept
// sorted so machine consumers can compare or hash the document canonically.
func GetCapabilities() Capabilities {
	operations := []string{
		"package.build.v1",
		"package.check.v1",
		"package.init.v1",
		"package.parse.v1",
		"repository.compose-metadata.v1",
		"repository.delete.v1",
		"repository.inspect.v1",
		"repository.pack-empty.v1",
		"repository.pack.v1",
		"repository.publish-client-signed-tparun-ssh.v1",
		"repository.publish-client-signed-tparun-ssh.v2",
		"repository.publish-ssh-new.v1",
		"repository.sign-staged-owner-side.v1",
		"repository.stage-tparun-cas.v1",
		"repository.unlist.v1",
		"repository.verify.v1",
	}
	readers := []string{"http", "https", "local", "ssh-read-only"}
	return Capabilities{
		Format:     RepositoryCapabilitiesName,
		Version:    RepositoryCapabilitiesV1,
		TPAVersion: version.Version,
		Platform:   CapabilityPlatform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		RepositoryFormat: CapabilityRepository{
			Name:                            RepositoryFormatName,
			Version:                         RepositoryFormatVersion,
			BrowserIndex:                    RepositoryBrowserFormat,
			BrowserIndexVersion:             RepositoryBrowserVersion,
			BrowserSidecarsPaired:           true,
			BrowserSidecarsRequired:         true,
			BrowserSidecarsCoveredByRelease: false,
			EmptyInitialization:             repositoryInitializationSupported(),
		},
		Operations: operations,
		Readers:    readers,
		Signing:    CapabilitySigning{GPGInRelease: true, ManagedKeys: false},
		Publication: CapabilityPublication{
			AtomicReplaceLinuxRenameExchange: runtime.GOOS == "linux",
			HostedManagedPublication:         false,
			ClientSignedSSH:                  true, ClientSignedSSHProtocol: "tparun-chunk-ssh", ClientSignedSSHProtocolVersion: 2, ClientSignedSSHChunkedCAS: true,
		},
		Transport: CapabilityTransport{InboundSSHUpload: false, AuthenticatedUpload: false},
		OutputBackends: []CapabilityOutputBackend{
			{
				Name: "local-filesystem", ExistingDestinationReplacement: runtime.GOOS == "linux", AtomicActivationGuarantee: "platform-dependent; Linux atomic replacement uses renameat2(RENAME_EXCHANGE)",
			},
			{
				Name: "ssh-sftp", RequiresOpenSSHClient: true, StrictHostKeyChecking: true,
				NewDestinationOnly: true, ExistingDestinationReplacement: false, TPAWriterLock: true,
				AtomicActivationGuarantee: "server-dependent same-parent SFTP directory rename; remote parent must exclude out-of-band writers",
			},
		},
	}
}
