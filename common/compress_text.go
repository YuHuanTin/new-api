package common

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"strings"
)

// compressedTextPrefix 标记一段文本是 gzip+base64 编码过的
const compressedTextPrefix = "gz:"

// CompressText 把文本压成 "gz:" + base64(gzip(text))
//
// 落库的 logs.other 是 TEXT 列里的 JSON，JSON 字符串必须是合法 UTF-8，gzip
// 出来的字节流存不进去，所以只能再套一层 base64。报文类文本压缩率通常在 4 倍
// 以上，扣掉 base64 的 33% 仍然明显省空间
func CompressText(text string) string {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(text)); err != nil {
		_ = writer.Close()
		return text
	}
	if err := writer.Close(); err != nil {
		return text
	}
	return compressedTextPrefix + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

// DecompressText 还原 CompressText 的结果，不带标记的文本原样返回
func DecompressText(text string) string {
	encoded, ok := strings.CutPrefix(text, compressedTextPrefix)
	if !ok {
		return text
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return text
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return text
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return text
	}
	return string(decoded)
}
