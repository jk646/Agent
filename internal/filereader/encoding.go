package filereader

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func decodeText(data []byte) (string, string, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		data = data[3:]
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return "", "", ErrUnsupportedEncoding
		}
		return string(data), "utf-8-bom", nil
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		decoded, err := decodeUTF16(data[2:], binary.LittleEndian)
		return decoded, "utf-16le", err
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		decoded, err := decodeUTF16(data[2:], binary.BigEndian)
		return decoded, "utf-16be", err
	default:
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return "", "", ErrUnsupportedEncoding
		}
		return string(data), "utf-8", nil
	}
}

func decodeUTF16(data []byte, order binary.ByteOrder) (string, error) {
	if len(data)%2 != 0 {
		return "", ErrUnsupportedEncoding
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = order.Uint16(data[index*2:])
	}
	for index := 0; index < len(units); index++ {
		switch {
		case units[index] >= 0xd800 && units[index] <= 0xdbff:
			if index+1 >= len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
				return "", ErrUnsupportedEncoding
			}
			index++
		case units[index] >= 0xdc00 && units[index] <= 0xdfff:
			return "", ErrUnsupportedEncoding
		}
	}
	return string(utf16.Decode(units)), nil
}

func detectEncoding(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		return "utf-8-bom"
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		return "utf-16le"
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		return "utf-16be"
	case utf8.Valid(data) && bytes.IndexByte(data, 0) < 0:
		return "utf-8"
	default:
		return "binary"
	}
}

func detectEncodingFromFile(path string) string {
	handle, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer handle.Close()
	buffer := make([]byte, 4096)
	count, _ := handle.Read(buffer)
	return detectEncoding(buffer[:count])
}

func detectNewline(content string) string {
	crlf := strings.Count(content, "\r\n")
	lf := strings.Count(content, "\n")
	switch {
	case lf == 0:
		return "none"
	case crlf == lf:
		return "crlf"
	case crlf == 0:
		return "lf"
	default:
		return "mixed"
	}
}
