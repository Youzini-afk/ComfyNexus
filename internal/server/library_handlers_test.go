package server

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func TestPNGTextChunksParsesTextAndInternationalText(t *testing.T) {
	png := buildPNG(
		pngChunk("tEXt", append(append([]byte("prompt"), 0), []byte("a comfy prompt")...)),
		pngChunk("iTXt", append(append([]byte("workflow"), 0, 0, 0, 0, 0), []byte(`{"nodes":[]}`)...)),
		pngChunk("IEND", nil),
	)

	chunks := pngTextChunks(png)
	if chunks["prompt"] != "a comfy prompt" {
		t.Fatalf("prompt chunk = %q", chunks["prompt"])
	}
	if chunks["workflow"] != `{"nodes":[]}` {
		t.Fatalf("workflow chunk = %q", chunks["workflow"])
	}
}

func TestPNGTextChunksRejectsInvalidPNG(t *testing.T) {
	chunks := pngTextChunks([]byte("not a png"))
	if len(chunks) != 0 {
		t.Fatalf("pngTextChunks() = %#v, want empty map", chunks)
	}
}

func TestParseITXtRejectsCompressedText(t *testing.T) {
	chunk := append(append([]byte("workflow"), 0, 1, 0, 0, 0), []byte("compressed")...)
	if key, text, ok := parseITXt(chunk); ok {
		t.Fatalf("parseITXt() = %q, %q, true; want false", key, text)
	}
}

func buildPNG(chunks ...[]byte) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	for _, chunk := range chunks {
		b.Write(chunk)
	}
	return b.Bytes()
}

func pngChunk(typ string, data []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
	b.WriteString(typ)
	b.Write(data)
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(typ))
	_, _ = crc.Write(data)
	_ = binary.Write(&b, binary.BigEndian, crc.Sum32())
	return b.Bytes()
}
