package filereader

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

type decodedFile struct {
	content  string
	encoding string
	newline  string
	sha256   string
}

func (m *Manager) Text(parent context.Context, params TextParams) (TextResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return TextResult{}, err
	}
	defer release()
	resolved, err := m.resolveAndAuthorize(ctx, "text", params.Path)
	if err != nil {
		return TextResult{}, err
	}
	file, err := m.readDecoded(ctx, resolved.Absolute)
	if err != nil {
		return TextResult{}, err
	}
	runes := []rune(file.content)
	start := params.StartChar
	end := params.EndChar
	if start < 0 || end < 0 || start > len(runes) || end > 0 && end < start {
		return TextResult{}, fmt.Errorf("%w: invalid character range", ErrInvalidRequest)
	}
	if end == 0 || end > len(runes) {
		end = len(runes)
	}
	maxBytes := m.outputLimit(params.MaxBytes)
	actualEnd := start
	var builder strings.Builder
	for actualEnd < end {
		if err := ctx.Err(); err != nil {
			return TextResult{}, err
		}
		encodedBytes := utf8.RuneLen(runes[actualEnd])
		if encodedBytes > maxBytes && builder.Len() == 0 {
			return TextResult{}, fmt.Errorf("%w: a single character exceeds max_bytes", ErrTooLarge)
		}
		if builder.Len()+encodedBytes > maxBytes {
			break
		}
		builder.WriteRune(runes[actualEnd])
		actualEnd++
	}
	truncated := actualEnd < end
	result := TextResult{
		ReadID: readID, Path: resolved.Relative, Content: builder.String(), Encoding: file.encoding,
		Newline: file.newline, SHA256: file.sha256, StartChar: start, EndChar: actualEnd,
		TotalChars: len(runes), Truncated: truncated,
	}
	if truncated {
		result.NextChar = actualEnd
	}
	return result, nil
}

func (m *Manager) Lines(parent context.Context, params LinesParams) (LinesResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return LinesResult{}, err
	}
	defer release()
	resolved, err := m.resolveAndAuthorize(ctx, "lines", params.Path)
	if err != nil {
		return LinesResult{}, err
	}
	file, err := m.readDecoded(ctx, resolved.Absolute)
	if err != nil {
		return LinesResult{}, err
	}
	lines := splitLines(file.content)
	start := params.StartLine
	if start <= 0 {
		start = 1
	}
	if start > len(lines)+1 {
		return LinesResult{}, fmt.Errorf("%w: start_line exceeds file length", ErrInvalidRequest)
	}
	end := params.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if end < start-1 {
		return LinesResult{}, fmt.Errorf("%w: invalid line range", ErrInvalidRequest)
	}
	maxBytes := m.outputLimit(params.MaxBytes)
	actualEnd := start - 1
	var builder strings.Builder
	for lineNumber := start; lineNumber <= end; lineNumber++ {
		if err := ctx.Err(); err != nil {
			return LinesResult{}, err
		}
		line := lines[lineNumber-1]
		if params.IncludeLineNumbers {
			line = fmt.Sprintf("%d: %s", lineNumber, line)
		}
		if len(line) > maxBytes && builder.Len() == 0 {
			return LinesResult{}, fmt.Errorf("%w: a single line exceeds max_bytes", ErrTooLarge)
		}
		if builder.Len()+len(line) > maxBytes {
			break
		}
		builder.WriteString(line)
		actualEnd = lineNumber
	}
	truncated := actualEnd < end
	result := LinesResult{
		ReadID: readID, Path: resolved.Relative, Content: builder.String(), Encoding: file.encoding,
		Newline: file.newline, SHA256: file.sha256, StartLine: start, EndLine: actualEnd,
		TotalLines: len(lines), Truncated: truncated,
	}
	if truncated {
		result.NextLine = actualEnd + 1
	}
	return result, nil
}

func (m *Manager) Bytes(parent context.Context, params BytesParams) (BytesResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return BytesResult{}, err
	}
	defer release()
	if params.Offset < 0 || params.Length < 0 {
		return BytesResult{}, fmt.Errorf("%w: invalid byte range", ErrInvalidRequest)
	}
	resolved, err := m.resolveAndAuthorize(ctx, "bytes", params.Path)
	if err != nil {
		return BytesResult{}, err
	}
	handle, err := os.Open(resolved.Absolute)
	if err != nil {
		return BytesResult{}, mapPathError(err)
	}
	defer handle.Close()
	before, err := handle.Stat()
	if err != nil {
		return BytesResult{}, err
	}
	if !before.Mode().IsRegular() {
		return BytesResult{}, ErrNotRegular
	}
	if params.Offset > before.Size() {
		return BytesResult{}, fmt.Errorf("%w: offset exceeds file size", ErrInvalidRequest)
	}
	length := params.Length
	if length <= 0 {
		length = 64 << 10
	}
	if length > m.cfg.MaxChunkBytes {
		return BytesResult{}, ErrTooLarge
	}
	buffer := make([]byte, length)
	count, readErr := handle.ReadAt(buffer, params.Offset)
	if readErr != nil && readErr != io.EOF {
		return BytesResult{}, readErr
	}
	if err := ctx.Err(); err != nil {
		return BytesResult{}, err
	}
	after, err := handle.Stat()
	if err != nil {
		return BytesResult{}, err
	}
	if !sameSnapshot(before, after) {
		return BytesResult{}, ErrFileChanged
	}
	next := params.Offset + int64(count)
	result := BytesResult{
		ReadID: readID, Path: resolved.Relative, Offset: params.Offset, BytesRead: count,
		DataBase64: base64.StdEncoding.EncodeToString(buffer[:count]), EOF: next >= before.Size(),
	}
	if !result.EOF {
		result.NextOffset = next
	}
	if params.IncludeHash {
		result.SHA256, err = hashFile(ctx, resolved.Absolute, before, m.cfg.MaxHashBytes)
		if err != nil {
			return BytesResult{}, err
		}
	}
	return result, nil
}

func (m *Manager) Hash(parent context.Context, params HashParams) (HashResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return HashResult{}, err
	}
	defer release()
	resolved, err := m.resolveAndAuthorize(ctx, "hash", params.Path)
	if err != nil {
		return HashResult{}, err
	}
	info, err := os.Stat(resolved.Absolute)
	if err != nil {
		return HashResult{}, mapPathError(err)
	}
	hash, err := hashFile(ctx, resolved.Absolute, info, m.cfg.MaxHashBytes)
	if err != nil {
		return HashResult{}, err
	}
	return HashResult{ReadID: readID, Path: resolved.Relative, Size: info.Size(), SHA256: hash}, nil
}

func (m *Manager) readDecoded(ctx context.Context, path string) (decodedFile, error) {
	before, err := os.Stat(path)
	if err != nil {
		return decodedFile{}, mapPathError(err)
	}
	if !before.Mode().IsRegular() {
		return decodedFile{}, ErrNotRegular
	}
	if before.Size() > m.cfg.MaxTextBytes {
		return decodedFile{}, ErrTooLarge
	}
	handle, err := os.Open(path)
	if err != nil {
		return decodedFile{}, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, m.cfg.MaxTextBytes+1))
	if err != nil {
		return decodedFile{}, err
	}
	if int64(len(data)) > m.cfg.MaxTextBytes {
		return decodedFile{}, ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return decodedFile{}, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return decodedFile{}, err
	}
	if !sameSnapshot(before, after) {
		return decodedFile{}, ErrFileChanged
	}
	content, encoding, err := decodeText(data)
	if err != nil {
		return decodedFile{}, err
	}
	hash := sha256.Sum256(data)
	return decodedFile{content: content, encoding: encoding, newline: detectNewline(content), sha256: hex.EncodeToString(hash[:])}, nil
}

func (m *Manager) outputLimit(requested int) int {
	if requested <= 0 {
		requested = 256 << 10
	}
	if requested > m.cfg.MaxChunkBytes {
		return m.cfg.MaxChunkBytes
	}
	return requested
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func hashFile(ctx context.Context, path string, before os.FileInfo, maxBytes int64) (string, error) {
	if !before.Mode().IsRegular() {
		return "", ErrNotRegular
	}
	if before.Size() > maxBytes {
		return "", ErrTooLarge
	}
	handle, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	hash := sha256.New()
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := handle.Read(buffer)
		if count > 0 {
			total += int64(count)
			if total > maxBytes {
				return "", ErrTooLarge
			}
			_, _ = hash.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	after, err := handle.Stat()
	if err != nil {
		return "", err
	}
	if !sameSnapshot(before, after) {
		return "", ErrFileChanged
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sameSnapshot(before, after os.FileInfo) bool {
	return before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && before.Mode() == after.Mode()
}
