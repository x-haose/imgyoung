package channel

import (
	"errors"
	"strings"
	"testing"
)

// TestHumanizeProbeErrorByMeaning 验证探测错误按真实含义归类:只有 S3 的凭证错误提示 Access Key,
// WebDAV 的 401、网盘的令牌失效与无权限各得其文案,不串用。
// @Description: 错误原文取自各后端的实际格式(AWS SDK、rclone webdav、rclone oauthutil、Graph API)。
func TestHumanizeProbeErrorByMeaning(t *testing.T) {
	cases := map[string]struct {
		raw       string
		accessKey bool   // 文案是否应提示 Access Key
		contains  string // 文案应含的定位词
	}{
		"S3 拒绝访问": {
			raw: "operation error S3: PutObject, https response error StatusCode: 403, RequestID: R, " +
				"api error AccessDenied: Access Denied",
			accessKey: true, contains: "403",
		},
		"S3 密钥不存在": {
			raw: "operation error S3: HeadObject, https response error StatusCode: 403, RequestID: R, " +
				"api error InvalidAccessKeyId: The AWS Access Key Id you provided does not exist",
			accessKey: true, contains: "403",
		},
		"WebDAV 密码错误":   {raw: `写入对象 "probe": 401 Unauthorized`, contains: "401"},
		"OneDrive 令牌无效": {raw: "InvalidAuthenticationToken: Access token has expired", contains: "401"},
		"OneDrive 无权限":  {raw: "accessDenied: Access denied", contains: "403"},
		"刷新令牌被拒": {
			raw:      `couldn't fetch token: invalid_grant: maybe token expired? - try refreshing`,
			contains: "重新授权",
		},
		"自建应用凭证错误": {
			raw:      "couldn't fetch token: invalid_client: if you're using your own client id/secret",
			contains: "Client Secret",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := humanizeProbeError(errors.New(tc.raw))
			if strings.Contains(got, "Access Key") != tc.accessKey {
				t.Errorf("文案 %q 对 Access Key 的提示与后端不符", got)
			}
			if !strings.Contains(got, tc.contains) {
				t.Errorf("文案 %q 未含 %q", got, tc.contains)
			}
		})
	}
}
