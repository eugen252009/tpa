package aptpackage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"

	"github.com/ulikunitz/xz/lzma"
)

const (
	maxXZDictionarySize = 16 << 20
	maxXZBlocks         = 4096
)

var xzMagic = [6]byte{0xfd, '7', 'z', 'X', 'Z', 0x00}

type xzBlockInfo struct {
	offset             int64
	header             []byte
	compressedSize     uint64
	hasCompressedSize  bool
	headerUncompressed uint64
	hasUncompressed    bool
	dictionaryProp     int
	dictionarySize     int64
	unsupported        string
}

type xzIndexRecord struct {
	unpaddedSize     uint64
	uncompressedSize uint64
}

type xzBytePatch struct {
	offset int64
	value  byte
}

// prepareXZReader validates XZ framing and bounds decoder dictionaries before
// passing data to the pure-Go decoder. The on-disk dictionary declaration can
// be larger than the control archive needs, so the equivalent smallest safe
// dictionary (bounded by the block's indexed uncompressed size) is patched into
// the in-memory header view and its header CRC is recomputed.
func prepareXZReader(file *os.File, start, size int64) (io.Reader, error) {
	if size < 24 {
		return nil, fmt.Errorf("truncated XZ control archive")
	}
	offset := int64(0)
	patches := make([]xzBytePatch, 0, 8)
	for streams := 0; offset < size; streams++ {
		if streams >= 16 {
			return nil, &unsupportedDebFormatError{reason: "too many concatenated XZ streams"}
		}
		if size-offset < 12 {
			return nil, fmt.Errorf("truncated XZ stream header")
		}
		var streamHeader [12]byte
		if _, err := file.ReadAt(streamHeader[:], start+offset); err != nil {
			return nil, fmt.Errorf("read XZ stream header: %w", err)
		}
		if !bytes.Equal(streamHeader[:6], xzMagic[:]) {
			return nil, fmt.Errorf("invalid XZ stream magic")
		}
		if streamHeader[6] != 0 || streamHeader[7]&0xf0 != 0 {
			return nil, fmt.Errorf("invalid XZ stream flags")
		}
		if crc32.ChecksumIEEE(streamHeader[6:8]) != binary.LittleEndian.Uint32(streamHeader[8:12]) {
			return nil, fmt.Errorf("invalid XZ stream header checksum")
		}
		checkSize, supportedCheck := xzCheckSize(streamHeader[7] & 0x0f)
		if !supportedCheck {
			return nil, &unsupportedDebFormatError{reason: "unsupported XZ integrity check"}
		}
		offset += 12
		blocks := make([]xzBlockInfo, 0, 2)
		for {
			if offset >= size {
				return nil, fmt.Errorf("truncated XZ block sequence")
			}
			var first [1]byte
			if _, err := file.ReadAt(first[:], start+offset); err != nil {
				return nil, fmt.Errorf("read XZ block header: %w", err)
			}
			if first[0] == 0 {
				break
			}
			headerSize := int64(first[0]+1) * 4
			if headerSize < 8 || headerSize > 1024 || headerSize > size-offset {
				return nil, fmt.Errorf("invalid XZ block header size")
			}
			header := make([]byte, int(headerSize))
			if _, err := file.ReadAt(header, start+offset); err != nil {
				return nil, fmt.Errorf("read XZ block header: %w", err)
			}
			block, err := parseXZBlockHeader(header)
			if err != nil {
				return nil, err
			}
			if block.unsupported != "" {
				return nil, &unsupportedDebFormatError{reason: block.unsupported}
			}
			if !block.hasCompressedSize {
				return nil, &unsupportedDebFormatError{reason: "XZ block omits compressed size"}
			}
			unpadded := uint64(headerSize) + block.compressedSize + uint64(checkSize)
			if unpadded < block.compressedSize || unpadded > uint64(size-offset) {
				return nil, fmt.Errorf("truncated XZ block data")
			}
			padded := (unpadded + 3) &^ uint64(3)
			if padded > uint64(size-offset) {
				return nil, fmt.Errorf("truncated XZ block padding")
			}
			block.offset = offset
			block.header = header
			blocks = append(blocks, block)
			if len(blocks) > maxXZBlocks {
				return nil, &unsupportedDebFormatError{reason: "too many XZ blocks"}
			}
			offset += int64(padded)
		}
		indexStart := offset
		indexBytes, indexRecords, err := readXZIndex(file, start, size, &offset)
		if err != nil {
			return nil, err
		}
		if len(indexRecords) != len(blocks) {
			return nil, fmt.Errorf("XZ index block count mismatch")
		}
		if crc32.ChecksumIEEE(indexBytes[:len(indexBytes)-4]) != binary.LittleEndian.Uint32(indexBytes[len(indexBytes)-4:]) {
			return nil, fmt.Errorf("invalid XZ index checksum")
		}
		for i, block := range blocks {
			unpadded := uint64(len(block.header)) + block.compressedSize + uint64(checkSize)
			if indexRecords[i].unpaddedSize != unpadded {
				return nil, fmt.Errorf("XZ index block size mismatch")
			}
			if block.hasUncompressed && block.headerUncompressed != indexRecords[i].uncompressedSize {
				return nil, fmt.Errorf("XZ block uncompressed size mismatch")
			}
			if block.dictionaryProp < 0 {
				continue
			}
			targetSize := indexRecords[i].uncompressedSize
			if targetSize > uint64(block.dictionarySize) {
				targetSize = uint64(block.dictionarySize)
			}
			if targetSize > maxXZDictionarySize {
				return nil, &unsupportedDebFormatError{reason: "XZ dictionary exceeds in-process memory limit"}
			}
			target := int64(targetSize)
			if target < lzma.MinDictCap {
				target = lzma.MinDictCap
			}
			property := lzma.EncodeDictCap(target)
			if property != block.header[block.dictionaryProp] {
				block.header[block.dictionaryProp] = property
				checksum := crc32.ChecksumIEEE(block.header[:len(block.header)-4])
				binary.LittleEndian.PutUint32(block.header[len(block.header)-4:], checksum)
				patches = append(patches,
					xzBytePatch{offset: block.offset + int64(block.dictionaryProp), value: property},
				)
				for i, value := range block.header[len(block.header)-4:] {
					patches = append(patches, xzBytePatch{offset: block.offset + int64(len(block.header)-4+i), value: value})
				}
			}
		}
		indexSize := offset - indexStart
		if size-offset < 12 {
			return nil, fmt.Errorf("truncated XZ stream footer")
		}
		var footer [12]byte
		if _, err := file.ReadAt(footer[:], start+offset); err != nil {
			return nil, fmt.Errorf("read XZ stream footer: %w", err)
		}
		if footer[10] != 'Y' || footer[11] != 'Z' || crc32.ChecksumIEEE(footer[4:10]) != binary.LittleEndian.Uint32(footer[:4]) {
			return nil, fmt.Errorf("invalid XZ stream footer")
		}
		if !bytes.Equal(footer[8:10], streamHeader[6:8]) {
			return nil, fmt.Errorf("XZ stream flags mismatch")
		}
		backwardSize := uint64(binary.LittleEndian.Uint32(footer[4:8]))
		if (backwardSize+1)*4 != uint64(indexSize) {
			return nil, fmt.Errorf("XZ stream index size mismatch")
		}
		offset += 12
		if offset == size {
			return &xzPatchedReader{source: io.NewSectionReader(file, start, size), patches: patches}, nil
		}
		paddingStart := offset
		for offset < size {
			var next [1]byte
			if _, err := file.ReadAt(next[:], start+offset); err != nil {
				return nil, fmt.Errorf("read XZ stream padding: %w", err)
			}
			if next[0] != 0 {
				break
			}
			offset++
		}
		if (offset-paddingStart)%4 != 0 {
			return nil, fmt.Errorf("invalid XZ stream padding")
		}
		if offset == size {
			return nil, &unsupportedDebFormatError{reason: "XZ stream padding"}
		}
		if size-offset < int64(len(xzMagic)) {
			return nil, fmt.Errorf("trailing bytes after XZ stream")
		}
		var nextMagic [6]byte
		if _, err := file.ReadAt(nextMagic[:], start+offset); err != nil {
			return nil, fmt.Errorf("read concatenated XZ stream: %w", err)
		}
		if !bytes.Equal(nextMagic[:], xzMagic[:]) {
			return nil, fmt.Errorf("trailing bytes after XZ stream")
		}
		return nil, &unsupportedDebFormatError{reason: "concatenated XZ streams"}
	}
	return nil, nil
}

type xzPatchedReader struct {
	source     io.Reader
	patches    []xzBytePatch
	patchIndex int
	pos        int64
}

func (r *xzPatchedReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if n > 0 {
		end := r.pos + int64(n)
		for r.patchIndex < len(r.patches) && r.patches[r.patchIndex].offset < end {
			patch := r.patches[r.patchIndex]
			if patch.offset >= r.pos {
				p[patch.offset-r.pos] = patch.value
			}
			r.patchIndex++
		}
		r.pos = end
	}
	return n, err
}

func parseXZBlockHeader(header []byte) (xzBlockInfo, error) {
	block := xzBlockInfo{dictionaryProp: -1}
	if len(header) < 8 || int(header[0]+1)*4 != len(header) {
		return block, fmt.Errorf("invalid XZ block header")
	}
	if crc32.ChecksumIEEE(header[:len(header)-4]) != binary.LittleEndian.Uint32(header[len(header)-4:]) {
		return block, fmt.Errorf("invalid XZ block header checksum")
	}
	flags := header[1]
	if flags&0x3c != 0 {
		return block, fmt.Errorf("invalid XZ block flags")
	}
	filters := int(flags&0x03) + 1
	block.hasCompressedSize = flags&0x40 != 0
	block.hasUncompressed = flags&0x80 != 0
	pos := 2
	if block.hasCompressedSize {
		var next int
		var err error
		block.compressedSize, next, err = readXZVLI(header[:len(header)-4], pos)
		if err != nil {
			return block, fmt.Errorf("malformed XZ compressed size: %w", err)
		}
		pos = next
	}
	if block.hasUncompressed {
		var next int
		var err error
		block.headerUncompressed, next, err = readXZVLI(header[:len(header)-4], pos)
		if err != nil {
			return block, fmt.Errorf("malformed XZ uncompressed size: %w", err)
		}
		pos = next
	}
	unsupportedFilter := false
	lzma2Found := false
	for i := 0; i < filters; i++ {
		filterID, next, err := readXZVLI(header[:len(header)-4], pos)
		if err != nil {
			return block, fmt.Errorf("malformed XZ filter ID: %w", err)
		}
		pos = next
		propertiesSize, next, err := readXZVLI(header[:len(header)-4], pos)
		if err != nil {
			return block, fmt.Errorf("malformed XZ filter properties: %w", err)
		}
		pos = next
		if pos > len(header)-4 || propertiesSize > uint64(len(header)-4-pos) {
			return block, fmt.Errorf("truncated XZ filter properties")
		}
		propertiesOffset := pos
		properties := header[pos : pos+int(propertiesSize)]
		pos += int(propertiesSize)
		if filterID == 0x21 {
			if lzma2Found || len(properties) != 1 {
				return block, fmt.Errorf("invalid XZ LZMA2 filter")
			}
			lzma2Found = true
			block.dictionarySize, err = lzma.DecodeDictCap(properties[0])
			if err != nil {
				return block, fmt.Errorf("invalid XZ LZMA2 dictionary size: %w", err)
			}
			block.dictionaryProp = propertiesOffset
		} else {
			unsupportedFilter = true
		}
	}
	for pos < len(header)-4 {
		if header[pos] != 0 {
			return block, fmt.Errorf("nonzero XZ block-header padding")
		}
		pos++
	}
	if !lzma2Found {
		unsupportedFilter = true
	}
	if unsupportedFilter {
		block.unsupported = "XZ filter chain is not LZMA2-only"
	}
	return block, nil
}

func readXZIndex(file *os.File, start, size int64, offset *int64) ([]byte, []xzIndexRecord, error) {
	indexStart := *offset
	var data []byte
	readByte := func() (byte, error) {
		if *offset >= size {
			return 0, io.ErrUnexpectedEOF
		}
		var b [1]byte
		if _, err := file.ReadAt(b[:], start+*offset); err != nil {
			return 0, err
		}
		*offset++
		data = append(data, b[0])
		return b[0], nil
	}
	indicator, err := readByte()
	if err != nil || indicator != 0 {
		return nil, nil, fmt.Errorf("malformed XZ index indicator")
	}
	count, err := readXZVLIReader(readByte)
	if err != nil {
		return nil, nil, fmt.Errorf("malformed XZ index record count: %w", err)
	}
	if count > maxXZBlocks {
		return nil, nil, &unsupportedDebFormatError{reason: "too many XZ index records"}
	}
	records := make([]xzIndexRecord, int(count))
	for i := range records {
		records[i].unpaddedSize, err = readXZVLIReader(readByte)
		if err != nil {
			return nil, nil, fmt.Errorf("malformed XZ index record: %w", err)
		}
		records[i].uncompressedSize, err = readXZVLIReader(readByte)
		if err != nil {
			return nil, nil, fmt.Errorf("malformed XZ index record: %w", err)
		}
	}
	for (*offset-indexStart+4)%4 != 0 {
		padding, err := readByte()
		if err != nil || padding != 0 {
			return nil, nil, fmt.Errorf("malformed XZ index padding")
		}
	}
	for i := 0; i < 4; i++ {
		if _, err := readByte(); err != nil {
			return nil, nil, fmt.Errorf("truncated XZ index checksum")
		}
	}
	return data, records, nil
}

func readXZVLI(data []byte, pos int) (uint64, int, error) {
	value, err := readXZVLIReader(func() (byte, error) {
		if pos >= len(data) {
			return 0, io.ErrUnexpectedEOF
		}
		b := data[pos]
		pos++
		return b, nil
	})
	return value, pos, err
}

func readXZVLIReader(readByte func() (byte, error)) (uint64, error) {
	var value uint64
	for i := 0; i < 9; i++ {
		b, err := readByte()
		if err != nil {
			return 0, err
		}
		if i == 8 && b&0x80 != 0 {
			return 0, fmt.Errorf("XZ variable integer is too long")
		}
		value |= uint64(b&0x7f) << uint(7*i)
		if b&0x80 == 0 {
			if i > 0 && b == 0 {
				return 0, fmt.Errorf("non-canonical XZ variable integer")
			}
			return value, nil
		}
	}
	return 0, fmt.Errorf("XZ variable integer is too long")
}

func xzCheckSize(id byte) (int, bool) {
	switch id {
	case 0:
		return 0, true
	case 1:
		return 4, true
	case 4:
		return 8, true
	case 10:
		return 32, true
	default:
		return 0, false
	}
}
