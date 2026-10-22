// Package signedurl 为私有图片的访问链接签名与验签:签名覆盖图片短 ID 与到期时刻,密钥由主密钥派生。
package signedurl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// 链接上的查询参数名。
const (
	ParamExpires   = "exp" // 到期时刻,Unix 秒,十进制规范写法
	ParamSignature = "sig" // 签名,无填充的 base64url
)

// keyPurpose 签名密钥的派生用途;换版本号即让此前签发的全部链接失效。
const keyPurpose = "signed-url:v1"

// keyLen 派生的签名密钥字节数。
const keyLen = 32

// ErrInvalid 签名缺失、不匹配或已过期。三种情况不区分,避免给探测者提示。
var ErrInvalid = errors.New("signedurl: 签名无效或已过期")

// Signer 签发与验证私有图片链接的签名,只读、并发安全。
type Signer struct {
	key []byte // HMAC-SHA256 密钥,来自主密钥派生
}

// New 由主密钥派生签名密钥。
// @param master 主密钥
// @return 签名器
// @return 派生失败时的错误
func New(master *secret.MasterKey) (*Signer, error) {
	key, err := master.Derive(keyPurpose, keyLen)
	if err != nil {
		return nil, fmt.Errorf("signedurl: %w", err)
	}
	return &Signer{key: key}, nil
}

// Query 生成图片链接要附加的查询串。
// @param imageID 图片短 ID
// @param expires 链接的到期时刻,精确到秒;此刻之后验签失败
// @return 形如 exp=…&sig=… 的查询串
func (s *Signer) Query(imageID string, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	v := url.Values{ParamExpires: {exp}, ParamSignature: {s.mac(imageID, exp)}}
	return v.Encode()
}

// Verify 验证链接上的签名。
// @Description: 到期时刻只认十进制规范写法,同一份签名不存在第二种等价写法;规范写法里没有分隔符,
// 签名消息里的图片 ID 与到期时刻之间不会因拼接产生歧义。
// @param imageID 图片短 ID
// @param exp 链接上的 exp 参数
// @param sig 链接上的 sig 参数
// @param now 当前时刻
// @return 验证失败时为 ErrInvalid
func (s *Signer) Verify(imageID, exp, sig string, now time.Time) error {
	n, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != exp {
		return ErrInvalid
	}
	if !hmac.Equal([]byte(sig), []byte(s.mac(imageID, exp))) || now.Unix() > n {
		return ErrInvalid
	}
	return nil
}

// mac 计算签名。
// @param imageID 图片短 ID
// @param exp 到期时刻的规范文本
// @return base64url 编码的签名
func (s *Signer) mac(imageID, exp string) string {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte("v1\n" + imageID + "\n" + exp))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
