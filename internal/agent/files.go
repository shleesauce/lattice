package agent

import (
	"context"
	"encoding/base64"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// listFiles lists a directory and pushes a file_list_result. An empty path
// resolves to the agent's home directory. Per-entry stat errors are tolerated.
func listFiles(ctx context.Context, p proto.FileReqPayload, outbound chan<- []byte) {
	result := proto.FileListResultPayload{ReqID: p.ReqID}

	dir, err := resolveListPath(p.Path)
	if err != nil {
		result.Error = err.Error()
		sendFrame(ctx, outbound, proto.TypeFileListResult, result)
		return
	}
	result.Path = dir
	result.Parent = filepath.Dir(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		result.Error = err.Error()
		sendFrame(ctx, outbound, proto.TypeFileListResult, result)
		return
	}

	out := make([]proto.FileEntry, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		fe := proto.FileEntry{
			Name:  e.Name(),
			Path:  full,
			IsDir: e.IsDir(),
		}
		if info, statErr := e.Info(); statErr == nil {
			fe.Size = info.Size()
			fe.ModTime = info.ModTime().UTC().Format(time.RFC3339)
			fe.IsDir = info.IsDir()
		}
		out = append(out, fe)
	}

	// Dirs first, then files; each group sorted case-insensitively by name.
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return lowerName(out[i].Name) < lowerName(out[j].Name)
	})
	result.Entries = out

	sendFrame(ctx, outbound, proto.TypeFileListResult, result)
}

// getFile reads a file (size-capped) and pushes a file_get_result.
func getFile(ctx context.Context, p proto.FileReqPayload, outbound chan<- []byte) {
	result := proto.FileGetResultPayload{ReqID: p.ReqID}

	if p.Path == "" {
		result.Error = "path is required"
		sendFrame(ctx, outbound, proto.TypeFileGetResult, result)
		return
	}

	// Rebase a synced under-home path (".../<home>/<rest>") onto THIS machine's
	// home (D23) so a download link built from the hub-side path still resolves on
	// a remote agent — same rule the session cwd + file listing use.
	abs, err := filepath.Abs(resolveCwd(p.Path))
	if err != nil {
		result.Error = err.Error()
		sendFrame(ctx, outbound, proto.TypeFileGetResult, result)
		return
	}
	result.Path = abs
	result.Name = filepath.Base(abs)

	info, err := os.Stat(abs)
	if err != nil {
		result.Error = err.Error()
		sendFrame(ctx, outbound, proto.TypeFileGetResult, result)
		return
	}
	if info.IsDir() {
		result.Error = "path is a directory"
		sendFrame(ctx, outbound, proto.TypeFileGetResult, result)
		return
	}
	result.Size = info.Size()

	f, err := os.Open(abs)
	if err != nil {
		result.Error = err.Error()
		sendFrame(ctx, outbound, proto.TypeFileGetResult, result)
		return
	}
	defer f.Close()

	// Read at most FileGetMaxBytes. A larger file is truncated, not refused, so
	// the operator still gets a preview.
	limited := io.LimitReader(f, proto.FileGetMaxBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		result.Error = err.Error()
		sendFrame(ctx, outbound, proto.TypeFileGetResult, result)
		return
	}
	if info.Size() > proto.FileGetMaxBytes {
		result.Truncated = true
	}
	result.Content = base64.StdEncoding.EncodeToString(data)

	sendFrame(ctx, outbound, proto.TypeFileGetResult, result)
}

// resolveListPath turns a request path into an absolute directory valid on THIS
// machine. Empty ⇒ home, and a synced under-home path (".../<home>/<rest>") is
// re-rooted at the local $HOME (D23) — so the dock file browser works for a
// session placed on any machine, not just the hub (the hub-side absolute path is
// wrong on a remote agent whose home differs). Reuses the session cwd resolver.
func resolveListPath(path string) (string, error) {
	return filepath.Abs(resolveCwd(path))
}

// sendFrame encodes a frame and pushes it through the outbound writer, dropping
// it if ctx is cancelled before the single-writer goroutine accepts it. This is
// the one place the agent's "encode → log → select{outbound; ctx.Done}" pattern
// lives; callers across the package route through it.
func sendFrame(ctx context.Context, outbound chan<- []byte, t proto.MessageType, payload any) {
	frame, err := proto.Encode(t, payload)
	if err != nil {
		log.Printf("agent: encode %s: %v", t, err)
		return
	}
	select {
	case outbound <- frame:
	case <-ctx.Done():
	}
}

// lowerName lowercases an ASCII-ish name for case-insensitive sort ordering.
func lowerName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
