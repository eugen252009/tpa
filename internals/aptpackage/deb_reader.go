package aptpackage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/ulikunitz/xz"
)

const (
	maxDebARMembers       = 4096
	maxDebianBinarySize   = 4096
	maxControlTarSize     = 64 << 20
	maxControlArchiveSize = 128 << 20
	maxControlFileSize    = 16 << 20
	maxControlTarEntries  = 4096
	maxControlTarPathSize = 4096
	maxDPKGControlOutput  = maxControlFileSize
	maxDPKGDiagnostics    = 64 << 10
)

var errControlLimit = errors.New("package control metadata exceeds the in-process safety limit")

type unsupportedDebFormatError struct {
	reason string
}

func (e *unsupportedDebFormatError) Error() string { return "unsupported package format: " + e.reason }

func readPackageControl(path string) ([]byte, error) {
	directStage := startStage(stageControlDirect)
	control, err := readPackageControlDirect(path)
	directStage()
	if err == nil {
		recordControlDirectRead()
		return control, nil
	}
	var unsupported *unsupportedDebFormatError
	if !errors.As(err, &unsupported) {
		return nil, fmt.Errorf("could not read package control: %w", err)
	}
	recordControlFallback(unsupported.reason)
	fallbackStage := startStage(stageControlFallback)
	control, fallbackErr := readPackageControlWithDPKG(path)
	fallbackStage()
	if fallbackErr != nil {
		return nil, fmt.Errorf("could not read package control (fallback for %s): %w", unsupported.reason, fallbackErr)
	}
	return control, nil
}

func readPackageControlWithDPKG(path string) ([]byte, error) {
	cmd := exec.Command("dpkg-deb", "-f", path)
	stdout := &boundedBuffer{limit: maxDPKGControlOutput}
	stderr := &truncatedBuffer{limit: maxDPKGDiagnostics}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, errControlLimit) {
			return nil, errControlLimit
		}
		diagnostic := strings.TrimSpace(stderr.String())
		if diagnostic != "" {
			return nil, fmt.Errorf("dpkg-deb -f: %w: %s", err, diagnostic)
		}
		return nil, fmt.Errorf("dpkg-deb -f: %w", err)
	}
	return stdout.Bytes(), nil
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errControlLimit
	}
	return b.buffer.Write(p)
}

func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }

type truncatedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *truncatedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buffer.Write(p[:remaining])
		} else {
			_, _ = b.buffer.Write(p)
		}
	}
	return len(p), nil
}

func (b *truncatedBuffer) String() string { return b.buffer.String() }

type debARMember struct {
	name   string
	offset int64
	size   int64
}

func readPackageControlDirect(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat package: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("package is not a regular file")
	}
	members, hasExtendedNames, err := readDebAR(file, info.Size())
	if err != nil {
		return nil, err
	}
	if hasExtendedNames {
		return nil, &unsupportedDebFormatError{reason: "extended ar member names"}
	}
	if len(members) < 3 {
		return nil, fmt.Errorf("ar package is missing required members")
	}
	if members[0].name != "debian-binary" {
		return nil, fmt.Errorf("ar package does not start with debian-binary")
	}
	versionBytes, err := readARMember(file, members[0], maxDebianBinarySize)
	if err != nil {
		return nil, fmt.Errorf("read debian-binary: %w", err)
	}
	if err := validateDebianBinary(versionBytes); err != nil {
		return nil, err
	}

	controlIndex, dataIndex := -1, -1
	controlCandidates, dataCandidates, debianBinaryCandidates := 0, 0, 0
	for i := range members {
		if members[i].name == "debian-binary" {
			debianBinaryCandidates++
		}
	}
	if debianBinaryCandidates != 1 {
		return nil, fmt.Errorf("ar package contains duplicate debian-binary members")
	}
	for i := 1; i < len(members); i++ {
		name := members[i].name
		if name == "control.tar" || strings.HasPrefix(name, "control.tar.") {
			controlCandidates++
			controlIndex = i
		}
		if name == "data.tar" || strings.HasPrefix(name, "data.tar.") {
			dataCandidates++
			dataIndex = i
		}
	}
	if controlCandidates == 0 {
		return nil, fmt.Errorf("ar package is missing control.tar")
	}
	if controlCandidates != 1 {
		return nil, fmt.Errorf("ar package contains ambiguous control.tar members")
	}
	if dataCandidates == 0 {
		return nil, fmt.Errorf("ar package is missing data.tar")
	}
	if dataCandidates != 1 {
		return nil, fmt.Errorf("ar package contains ambiguous data.tar members")
	}
	if controlIndex <= 0 || dataIndex <= controlIndex {
		return nil, fmt.Errorf("ar package members are not in Debian order")
	}
	if !supportedDataTarName(members[dataIndex].name) {
		return nil, &unsupportedDebFormatError{reason: "unsupported data archive compression"}
	}
	for i := 1; i < controlIndex; i++ {
		if !strings.HasPrefix(members[i].name, "_") {
			return nil, fmt.Errorf("unexpected ar member before control.tar: %s", members[i].name)
		}
	}
	for i := controlIndex + 1; i < dataIndex; i++ {
		if !strings.HasPrefix(members[i].name, "_") {
			return nil, fmt.Errorf("unexpected ar member between control.tar and data.tar: %s", members[i].name)
		}
	}
	if members[controlIndex].size > maxControlTarSize {
		return nil, errControlLimit
	}
	if members[controlIndex].size < 1 {
		return nil, fmt.Errorf("control.tar is empty")
	}

	controlName := members[controlIndex].name
	var archive io.Reader = io.NewSectionReader(file, members[controlIndex].offset, members[controlIndex].size)
	switch controlName {
	case "control.tar":
	case "control.tar.gz":
		gzipReader, err := gzip.NewReader(archive)
		if err != nil {
			return nil, fmt.Errorf("malformed control.tar.gz: %w", err)
		}
		defer gzipReader.Close()
		archive = gzipReader
	case "control.tar.xz":
		xzSource, err := prepareXZReader(file, members[controlIndex].offset, members[controlIndex].size)
		if err != nil {
			return nil, err
		}
		xzReader, err := (xz.ReaderConfig{DictCap: 4096}).NewReader(xzSource)
		if err != nil {
			return nil, fmt.Errorf("malformed control.tar.xz: %w", err)
		}
		archive = xzReader
	default:
		return nil, &unsupportedDebFormatError{reason: "unsupported control archive compression"}
	}
	return readControlTar(archive)
}

func readDebAR(file io.ReaderAt, fileSize int64) ([]debARMember, bool, error) {
	const magic = "!<arch>\n"
	var actualMagic [8]byte
	if _, err := file.ReadAt(actualMagic[:], 0); err != nil {
		return nil, false, fmt.Errorf("truncated ar archive: %w", err)
	}
	if string(actualMagic[:]) != magic {
		return nil, false, fmt.Errorf("invalid ar archive magic")
	}
	members := make([]debARMember, 0, 4)
	extendedNames := false
	for offset := int64(len(magic)); offset < fileSize; {
		if len(members) >= maxDebARMembers {
			return nil, false, fmt.Errorf("ar archive has too many members")
		}
		if fileSize-offset < 60 {
			return nil, false, fmt.Errorf("truncated ar member header")
		}
		var header [60]byte
		if _, err := file.ReadAt(header[:], offset); err != nil {
			return nil, false, fmt.Errorf("read ar member header: %w", err)
		}
		if string(header[58:60]) != "`\n" {
			return nil, false, fmt.Errorf("malformed ar member header trailer")
		}
		if _, err := parseARInteger(header[16:28], 10, "mtime"); err != nil {
			return nil, false, err
		}
		if _, err := parseARInteger(header[28:34], 10, "uid"); err != nil {
			return nil, false, err
		}
		if _, err := parseARInteger(header[34:40], 10, "gid"); err != nil {
			return nil, false, err
		}
		if _, err := parseARInteger(header[40:48], 8, "mode"); err != nil {
			return nil, false, err
		}
		size, err := parseARInteger(header[48:58], 10, "size")
		if err != nil || size < 0 {
			return nil, false, fmt.Errorf("invalid ar member size")
		}
		dataOffset := offset + 60
		if size > fileSize-dataOffset {
			return nil, false, fmt.Errorf("truncated ar member data")
		}
		name, extended := parseARName(header[:16])
		extendedNames = extendedNames || extended
		if name == "" {
			return nil, false, fmt.Errorf("empty ar member name")
		}
		if len(name) > 128 {
			return nil, false, fmt.Errorf("ar member name exceeds safety limit")
		}
		members = append(members, debARMember{name: name, offset: dataOffset, size: size})
		offset = dataOffset + size
		if size&1 != 0 {
			if offset >= fileSize {
				return nil, false, fmt.Errorf("truncated ar member padding")
			}
			var padding [1]byte
			if _, err := file.ReadAt(padding[:], offset); err != nil || padding[0] != '\n' {
				return nil, false, fmt.Errorf("malformed ar member padding")
			}
			offset++
		}
	}
	if len(members) == 0 || int64(len(members)) > fileSize {
		return nil, false, fmt.Errorf("empty ar archive")
	}
	return members, extendedNames, nil
}

func parseARInteger(field []byte, base int, label string) (int64, error) {
	value := strings.TrimSpace(string(field))
	if value == "" {
		return 0, fmt.Errorf("empty ar member %s", label)
	}
	for _, digit := range value {
		if digit < '0' || (base == 8 && digit > '7') || (base == 10 && digit > '9') {
			return 0, fmt.Errorf("invalid ar member %s", label)
		}
	}
	parsed, err := strconv.ParseInt(value, base, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid ar member %s", label)
	}
	return parsed, nil
}

func parseARName(field []byte) (string, bool) {
	name := strings.TrimSpace(string(field))
	if name == "//" || name == "/" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "#1/") {
		return name, true
	}
	return strings.TrimSuffix(name, "/"), false
}

func readARMember(file io.ReaderAt, member debARMember, maxSize int64) ([]byte, error) {
	if member.size > maxSize {
		return nil, errControlLimit
	}
	data := make([]byte, int(member.size))
	if _, err := file.ReadAt(data, member.offset); err != nil {
		return nil, err
	}
	return data, nil
}

func validateDebianBinary(data []byte) error {
	if len(data) == 0 || len(data) > maxDebianBinarySize || data[len(data)-1] != '\n' || bytes.IndexByte(data, 0) >= 0 {
		return fmt.Errorf("invalid debian-binary member")
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	if len(lines) == 0 {
		return fmt.Errorf("invalid debian-binary version")
	}
	version := bytes.SplitN(lines[0], []byte{'.'}, 2)
	if len(version) != 2 || len(version[0]) == 0 || len(version[1]) == 0 {
		return fmt.Errorf("invalid debian-binary version")
	}
	for _, part := range version {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return fmt.Errorf("invalid debian-binary version")
			}
		}
	}
	if string(version[0]) != "2" {
		return &unsupportedDebFormatError{reason: "debian-binary major version"}
	}
	for _, line := range lines[1:] {
		if len(line) == 0 {
			return fmt.Errorf("invalid empty debian-binary version line")
		}
	}
	return nil
}

func readControlTar(source io.Reader) ([]byte, error) {
	limited := &io.LimitedReader{R: source, N: maxControlArchiveSize + 1}
	tail := &tarTailReader{reader: limited}
	reader := tar.NewReader(tail)
	var control []byte
	found := false
	entries := 0
	var lastDataEnd int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed control tar: %w", err)
		}
		entries++
		if entries > maxControlTarEntries {
			return nil, fmt.Errorf("control tar has too many entries")
		}
		clean, err := validateControlTarPath(header.Name)
		if err != nil {
			return nil, err
		}
		if header.Size < 0 || header.Size > maxControlArchiveSize {
			return nil, fmt.Errorf("control tar entry exceeds safety limit")
		}
		lastDataEnd = tail.total + ((header.Size + 511) / 512 * 512)
		switch header.Typeflag {
		case tar.TypeDir:
		case tar.TypeReg, tar.TypeRegA:
		default:
			return nil, fmt.Errorf("unsupported control tar entry type %d", header.Typeflag)
		}
		if clean != "control" {
			continue
		}
		if found {
			return nil, fmt.Errorf("control tar contains duplicate control files")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("control tar control entry is not a regular file")
		}
		if header.Size < 0 || header.Size > maxControlFileSize {
			return nil, errControlLimit
		}
		control, err = io.ReadAll(io.LimitReader(reader, maxControlFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("read control file: %w", err)
		}
		if int64(len(control)) != header.Size {
			return nil, fmt.Errorf("truncated control file in control tar")
		}
		found = true
	}
	if tail.total < lastDataEnd+1024 {
		return nil, fmt.Errorf("control tar is missing end marker blocks")
	}
	trailing := &zeroOnlyWriter{allZero: true}
	if _, err := io.Copy(trailing, limited); err != nil {
		return nil, fmt.Errorf("finish control archive decompression: %w", err)
	}
	if !trailing.allZero || trailing.total%512 != 0 {
		return nil, fmt.Errorf("nonzero data after control tar end markers")
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("control archive exceeds safety limit")
	}
	if tail.total < 1024 || !tail.lastBytesZero(1024) {
		return nil, fmt.Errorf("malformed control tar end markers")
	}
	if !found {
		return nil, fmt.Errorf("control tar is missing control")
	}
	if len(control) == 0 {
		return nil, fmt.Errorf("control file is empty")
	}
	return control, nil
}

func validateControlTarPath(name string) (string, error) {
	if len(name) > maxControlTarPathSize {
		return "", fmt.Errorf("control tar path exceeds safety limit")
	}
	if name == "" || strings.ContainsRune(name, '\x00') || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe control tar path %q", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe control tar path %q", name)
	}
	return clean, nil
}

type tarTailReader struct {
	reader io.Reader
	last   [1024]byte
	total  int64
}

type zeroOnlyWriter struct {
	total   int64
	allZero bool
}

func (w *zeroOnlyWriter) Write(data []byte) (int, error) {
	if w.allZero && bytes.Count(data, []byte{0}) != len(data) {
		w.allZero = false
	}
	w.total += int64(len(data))
	return len(data), nil
}

func (r *tarTailReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n == 0 {
		return n, err
	}
	window := int64(len(r.last))
	startOffset := r.total
	data := p[:n]
	if len(data) >= len(r.last) {
		data = data[len(data)-len(r.last):]
		startOffset += int64(n - len(r.last))
	}
	start := int(startOffset % window)
	first := copy(r.last[start:], data)
	copy(r.last[:], data[first:])
	r.total += int64(n)
	return n, err
}

func (r *tarTailReader) lastBytesZero(count int) bool {
	if r.total < int64(count) {
		return false
	}
	start := r.total % int64(len(r.last))
	for i := 0; i < count; i++ {
		if r.last[(start+int64(i))%int64(len(r.last))] != 0 {
			return false
		}
	}
	return true
}

func supportedDataTarName(name string) bool {
	switch name {
	case "data.tar", "data.tar.gz", "data.tar.xz", "data.tar.zst", "data.tar.bz2", "data.tar.lzma":
		return true
	default:
		return false
	}
}
