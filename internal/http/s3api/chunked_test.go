package s3api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chunkFrame 按 aws-chunked 格式给一段数据加帧。
// @param sig 非空时写成带分块签名的形态(aws-cli 的签名流式模式)
func chunkFrame(data []byte, sig string) string {
	head := fmt.Sprintf("%x", len(data))
	if sig != "" {
		head += ";chunk-signature=" + sig
	}
	return head + "\r\n" + string(data) + "\r\n"
}

// TestDecodeAWSChunked 解帧还原真实正文:单块、多块、带分块签名、带尾部拖挂结果一致。
func TestDecodeAWSChunked(t *testing.T) {
	payload := []byte("the-actual-image-bytes")
	sig := strings.Repeat("a", 64)
	cases := map[string]string{
		"单块":    chunkFrame(payload, "") + "0\r\n\r\n",
		"多块":    chunkFrame(payload[:8], "") + chunkFrame(payload[8:], "") + "0\r\n\r\n",
		"带分块签名": chunkFrame(payload, sig) + "0;chunk-signature=" + sig + "\r\n\r\n",
		"带尾部拖挂": chunkFrame(payload, "") + "0\r\nx-amz-checksum-crc32:abc\r\n\r\n",
	}
	for name, framed := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := decodeAWSChunked(strings.NewReader(framed), 1<<20)
			if err != nil {
				t.Fatalf("解帧失败: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("解帧结果 = %q, 期望 %q", got, payload)
			}
		})
	}
}

// TestDecodeAWSChunkedRejectsMalformed 帧结构不合规一律报错,不返回半截内容。
func TestDecodeAWSChunkedRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"长度非十六进制":  "zz\r\ndata\r\n0\r\n\r\n",
		"数据短于声明":   "ff\r\nshort\r\n0\r\n\r\n",
		"缺块尾 CRLF": "04\r\ndataXX0\r\n\r\n",
		"缺收尾零长块":   chunkFrame([]byte("abc"), ""),
		"空正文":      "",
	}
	for name, framed := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeAWSChunked(strings.NewReader(framed), 1<<20); !errors.Is(err, errMalformedChunk) {
				t.Errorf("期望 errMalformedChunk,得 %v", err)
			}
		})
	}
}

// TestDecodeAWSChunkedEnforcesLimit 解帧后按真实内容长度判超限,不被分块拆分绕过。
func TestDecodeAWSChunkedEnforcesLimit(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 300)
	framed := chunkFrame(big[:100], "") + chunkFrame(big[100:], "") + "0\r\n\r\n"
	if _, err := decodeAWSChunked(strings.NewReader(framed), 200); !errors.Is(err, ErrTooLargeBody) {
		t.Fatalf("期望 ErrTooLargeBody,得 %v", err)
	}
}

// TestIsAWSChunked 两种声明方式都要认出来:Content-Encoding 与 STREAMING-* 正文哈希。
func TestIsAWSChunked(t *testing.T) {
	cases := map[string]struct {
		headers map[string]string
		want    bool
	}{
		"Content-Encoding": {map[string]string{"Content-Encoding": "aws-chunked"}, true},
		"编码含其他项":           {map[string]string{"Content-Encoding": "gzip, aws-chunked"}, true},
		"签名流式":             {map[string]string{contentSHAHeader: "STREAMING-AWS4-HMAC-SHA256-PAYLOAD"}, true},
		"无签名流式带拖挂":         {map[string]string{contentSHAHeader: "STREAMING-UNSIGNED-PAYLOAD-TRAILER"}, true},
		"普通正文":             {map[string]string{contentSHAHeader: strings.Repeat("0", 64)}, false},
		"未声明":              {map[string]string{}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/s3/b/a.png", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if got := isAWSChunked(req); got != tc.want {
				t.Errorf("isAWSChunked = %t, 期望 %t", got, tc.want)
			}
		})
	}
}
