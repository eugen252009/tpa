package aptpackage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const hostedChunkSize = 1 << 20
const hostedCASAlgorithm = "fixed-sha256-1m-v1"

type hostedChunk struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}
type hostedCASFile struct {
	Path   string        `json:"path"`
	Size   int64         `json:"size"`
	SHA256 string        `json:"sha256"`
	Chunks []hostedChunk `json:"chunks"`
}
type hostedCASInventory struct {
	Version          int             `json:"version"`
	Algorithm        string          `json:"algorithm"`
	RepositoryID     string          `json:"repository_id"`
	GenerationID     string          `json:"generation_id"`
	ParentGeneration string          `json:"parent_generation"`
	Files            []hostedCASFile `json:"files"`
}
type hostedChunkLocation struct {
	path         string
	offset, size int64
}

func hostedCASWrite(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > 16<<20 {
		return fmt.Errorf("CAS inventory bound exceeded")
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	if _, err = w.Write(n[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
func hostedCASRead(r io.Reader, v any) error {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size == 0 || size > 16<<20 {
		return fmt.Errorf("CAS response bound exceeded")
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	// Every framed phase can terminate with the server's error envelope. Decode
	// it before the phase-specific success shape; otherwise strict decoding hides
	// a legitimate rejection (e.g. stale parent) as an unknown-field failure.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(b, &envelope); err != nil {
		return err
	}
	if raw, ok := envelope["error"]; ok {
		var message string
		if len(envelope) != 1 || json.Unmarshal(raw, &message) != nil || message == "" {
			return fmt.Errorf("invalid CAS error envelope")
		}
		return fmt.Errorf("CAS rejected: %s", message)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("CAS trailing response")
	}
	return nil
}
func hostedCASInventoryFor(directory string, m GenerationManifest) (hostedCASInventory, map[string]hostedChunkLocation, error) {
	inv := hostedCASInventory{Version: 2, Algorithm: hostedCASAlgorithm, RepositoryID: m.RepositoryID, GenerationID: m.GenerationID, ParentGeneration: m.ParentGeneration}
	locations := make(map[string]hostedChunkLocation)
	buffer := make([]byte, hostedChunkSize)
	extents := 0
	for _, f := range m.Files {
		file, err := os.Open(filepath.Join(directory, filepath.FromSlash(f.Path)))
		if err != nil {
			return inv, nil, err
		}
		cf := hostedCASFile{Path: f.Path, Size: f.Size, SHA256: f.SHA256}
		whole := sha256.New()
		offset := int64(0)
		for {
			n, e := io.ReadFull(file, buffer)
			if e != nil && e != io.EOF && e != io.ErrUnexpectedEOF {
				file.Close()
				return inv, nil, e
			}
			if n > 0 {
				extents++
				if extents > 131072 {
					file.Close()
					return inv, nil, fmt.Errorf("CAS extent limit exceeded")
				}
				whole.Write(buffer[:n])
				sum := sha256.Sum256(buffer[:n])
				d := hex.EncodeToString(sum[:])
				cf.Chunks = append(cf.Chunks, hostedChunk{d, int64(n)})
				locations[d] = hostedChunkLocation{f.Path, offset, int64(n)}
				offset += int64(n)
			}
			if e == io.EOF || e == io.ErrUnexpectedEOF {
				break
			}
		}
		file.Close()
		if offset != f.Size || hex.EncodeToString(whole.Sum(nil)) != f.SHA256 {
			return inv, nil, fmt.Errorf("repository changed while chunking: %s", f.Path)
		}
		inv.Files = append(inv.Files, cf)
	}
	return inv, locations, nil
}

type hostedCountInput struct {
	io.WriteCloser
	n int64
}

func (w *hostedCountInput) Write(p []byte) (int, error) {
	n, e := w.WriteCloser.Write(p)
	w.n += int64(n)
	return n, e
}

type hostedCountOutput struct {
	io.Reader
	n int64
}

func (r *hostedCountOutput) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	r.n += int64(n)
	return n, e
}

func publishHostedCAS(directory string, m GenerationManifest, rawInput io.WriteCloser, rawOutput io.Reader) (HostedPublishResult, error) {
	input := &hostedCountInput{WriteCloser: rawInput}
	output := &hostedCountOutput{Reader: rawOutput}
	var result HostedPublishResult
	r := bufio.NewReader(output)
	line, err := r.ReadSlice('\n')
	if err != nil {
		return result, err
	}
	var ready struct {
		Version                    int
		Protocol, State, Algorithm string
	}
	if err := json.Unmarshal(line, &ready); err != nil {
		return result, err
	}
	if ready.Version != 2 || ready.Protocol != "tparun-chunk-ssh" || ready.State != "ready" || ready.Algorithm != hostedCASAlgorithm {
		return result, fmt.Errorf("required chunk-CAS protocol unavailable; no fallback")
	}
	inv, locations, err := hostedCASInventoryFor(directory, m)
	if err != nil {
		return result, err
	}
	if err := hostedCASWrite(input, inv); err != nil {
		return result, err
	}
	var need struct {
		Missing []hostedChunk `json:"missing"`
	}
	if err := hostedCASRead(r, &need); err != nil {
		return result, fmt.Errorf("CAS negotiation: %w", err)
	}
	seen := make(map[string]bool)
	for _, c := range need.Missing {
		loc, ok := locations[c.Digest]
		if !ok || c.Size != loc.size || seen[c.Digest] {
			return result, fmt.Errorf("server requested undeclared/duplicate chunk")
		}
		seen[c.Digest] = true
		f, err := os.Open(filepath.Join(directory, filepath.FromSlash(loc.path)))
		if err != nil {
			return result, err
		}
		n, err := io.Copy(input, io.NewSectionReader(f, loc.offset, loc.size))
		f.Close()
		if err != nil {
			return result, err
		}
		if n != loc.size {
			return result, fmt.Errorf("source chunk truncated")
		}
		var ack struct {
			Committed string `json:"committed"`
		}
		if err := hostedCASRead(r, &ack); err != nil {
			return result, err
		}
		if ack.Committed != c.Digest {
			return result, fmt.Errorf("wrong chunk commit acknowledgement")
		}
	}
	// EOF cannot be used as a delimiter for the frames; close the sending half only
	// after all exact-length chunks/acknowledgements, then await activation.
	if err := input.Close(); err != nil {
		return result, err
	}
	// Result has additive metering fields; keep the stable activation identity.
	var raw map[string]json.RawMessage
	if err := hostedCASRead(r, &raw); err != nil {
		return result, fmt.Errorf("CAS activation result unknown: %w", err)
	}
	if value, ok := raw["error"]; ok {
		var message string
		_ = json.Unmarshal(value, &message)
		return result, fmt.Errorf("CAS rejected: %s", message)
	}
	data, _ := json.Marshal(raw)
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.GenerationID != m.GenerationID || result.State != "succeeded" || !result.CAS {
		return result, fmt.Errorf("CAS activation result invalid")
	}
	result.SSHApplicationBytes = input.n + output.n
	result.ProtocolControlBytes = result.SSHApplicationBytes - result.UploadedBytes
	return result, nil
}
