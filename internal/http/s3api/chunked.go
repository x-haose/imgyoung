package s3api

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// awsChunkedEncoding 分块正文的 Content-Encoding 取值。
const awsChunkedEncoding = "aws-chunked"

// maxChunkHeaderBytes 单个分块头的长度上限;正常形如 "1000;chunk-signature=<64 hex>",
// 给足余量即可,超出即视为畸形帧而非合法输入。
const maxChunkHeaderBytes = 1024

// errMalformedChunk 分块正文的帧结构不合规。
var errMalformedChunk = errors.New("s3api: 分块正文格式非法")

// isAWSChunked 判定请求正文是否为 aws-chunked 分块编码。
// @Description: 客户端有两种表达:Content-Encoding 带 aws-chunked,或正文哈希声明为
// STREAMING-*;aws-cli v2 与新版 botocore 默认就走这条路,不解帧则整个客户端族不可用
// (带帧的字节进不了图片解码器,只会得到一句"不是图片")。
// @param r 收到的请求
// @return 是分块正文为 true
func isAWSChunked(r *http.Request) bool {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Encoding")), awsChunkedEncoding) {
		return true
	}
	return strings.HasPrefix(r.Header.Get(contentSHAHeader), "STREAMING-")
}

// decodeAWSChunked 解掉 aws-chunked 帧,返回真实正文。
// @Description: 帧格式为 `<hex 长度>[;chunk-signature=...]\r\n<数据>\r\n` 重复,以零长块收尾,
// 其后可能跟 trailer。本实现不校验逐块签名——整体签名已覆盖请求要素,分块签名链只在
// 流式转发场景有额外价值,而这里正文一次性读入内存后立刻交给图片解码器把关。
// @param r 原始正文流
// @param limit 解帧后正文的大小上限(字节)
// @return 去掉帧结构的正文
// @return 帧结构非法或超限时的错误
func decodeAWSChunked(r io.Reader, limit int64) ([]byte, error) {
	br := bufio.NewReader(r)
	var out []byte
	for {
		header, err := readChunkHeader(br)
		if err != nil {
			return nil, err
		}
		size, err := strconv.ParseInt(strings.TrimSpace(header), 16, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("%w: 块长度 %q", errMalformedChunk, header)
		}
		if size == 0 {
			return out, nil // 零长块即正文结束;其后的 trailer 不参与图片内容
		}
		if int64(len(out))+size > limit {
			return nil, ErrTooLargeBody
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, fmt.Errorf("%w: 块数据不足", errMalformedChunk)
		}
		out = append(out, buf...)
		// 每块数据后跟 CRLF;缺失说明长度声明与实际不符,不能继续按帧解析。
		if err := expectCRLF(br); err != nil {
			return nil, err
		}
	}
}

// readChunkHeader 读一行分块头(到 CRLF 为止),返回其长度字段部分。
// @param br 带缓冲的读取器
// @return 长度字段(十六进制文本)
// @return 读取失败或头过长时的错误
func readChunkHeader(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("%w: 读块头 %w", errMalformedChunk, err)
	}
	if len(line) > maxChunkHeaderBytes {
		return "", fmt.Errorf("%w: 块头过长", errMalformedChunk)
	}
	line = strings.TrimRight(line, "\r\n")
	// 分块签名紧跟在长度之后,以分号分隔;本实现只取长度。
	size, _, _ := strings.Cut(line, ";")
	return size, nil
}

// expectCRLF 读掉一个 CRLF。
// @param br 带缓冲的读取器
// @return 下两个字节不是 CRLF 时的错误
func expectCRLF(br *bufio.Reader) error {
	var crlf [2]byte
	if _, err := io.ReadFull(br, crlf[:]); err != nil || crlf[0] != '\r' || crlf[1] != '\n' {
		return fmt.Errorf("%w: 块尾缺少 CRLF", errMalformedChunk)
	}
	return nil
}
