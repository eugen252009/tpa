package aptpackage

type stageID uint8

const (
	stageDirectoryEnumeration stageID = iota
	stageInspect
	stageParseMetadata
	stageStatSource
	stageSourceHash
	stageDuplicateCompare
	stageSortAndGroup
	stageGroupArchitectures
	stagePoolMkdir
	stagePoolCopy
	stagePackagesWrite
	stageCompression
	stageReleaseIndexHash
	stageReleaseWrite
	stageSignKeyLookup
	stageSignCommand
	stageVerifyReleaseParse
	stageVerifyReleaseFiles
	stageVerifyIndexReadParse
	stageVerifyArtifacts
	stageVerifyHash
	stageVerifySignature
	stageBuildControlParse
	stageBuildScripts
	stageBuildOutputDirs
	stageDPKGStart
	stageDPKGWait
	stageManifestWalk
	stageManifestHash
	stageCount
)

var stageNames = [...]string{
	"directory_enumeration",
	"package_control_extraction",
	"metadata_parse_and_stanza",
	"source_stat",
	"source_hash",
	"duplicate_compare",
	"package_sort",
	"architecture_grouping",
	"pool_directory_creation",
	"pool_copy",
	"packages_generation",
	"compression",
	"release_index_hashing",
	"release_generation",
	"signing_key_lookup",
	"signing_command",
	"verification_release_parse",
	"verification_release_files",
	"verification_index_read_parse",
	"verification_artifacts",
	"verification_hash",
	"verification_signature",
	"build_control_parse",
	"build_scripts",
	"build_output_directories",
	"dpkg_deb_start",
	"dpkg_deb_wait",
	"manifest_walk_and_finalize",
	"manifest_file_hashing",
}
