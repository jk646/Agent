package textsearch

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"
)

type decodedText struct {
	text, encoding string
	bomBytes       int
}

func decodeText(data []byte) (decodedText, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		body := data[3:]
		if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
			return decodedText{}, ErrUnsupportedEncoding
		}
		return decodedText{text: string(body), encoding: "utf-8-bom", bomBytes: 3}, nil
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		text, err := decodeUTF16(data[2:], binary.LittleEndian)
		return decodedText{text: text, encoding: "utf-16le", bomBytes: 2}, err
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		text, err := decodeUTF16(data[2:], binary.BigEndian)
		return decodedText{text: text, encoding: "utf-16be", bomBytes: 2}, err
	default:
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return decodedText{}, ErrUnsupportedEncoding
		}
		return decodedText{text: string(data), encoding: "utf-8"}, nil
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
		if units[index] >= 0xd800 && units[index] <= 0xdbff {
			if index+1 >= len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
				return "", ErrUnsupportedEncoding
			}
			index++
		} else if units[index] >= 0xdc00 && units[index] <= 0xdfff {
			return "", ErrUnsupportedEncoding
		}
	}
	return string(utf16.Decode(units)), nil
}

func (d decodedText) originalOffset(decodedByteIndex int) int64 {
	if d.encoding == "utf-16le" || d.encoding == "utf-16be" {
		units := len(utf16.Encode([]rune(d.text[:decodedByteIndex])))
		return int64(d.bomBytes + units*2)
	}
	return int64(d.bomBytes + decodedByteIndex)
}
