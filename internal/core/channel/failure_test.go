package channel

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/x-haose/imgyoung/store/failover"
)

// TestExplainClassifiesByMeaning 验证失败归类按真实含义:只有 S3 的凭证错误归为 s3_auth,
// WebDAV 的 401、网盘的令牌失效与无权限各归各类,不串用。
// @Description: 错误原文取自各后端的实际格式(AWS SDK、rclone webdav、rclone oauthutil、Graph API)。
func TestExplainClassifiesByMeaning(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want FailureReason
	}{
		"S3 拒绝访问": {
			raw: "operation error S3: PutObject, https response error StatusCode: 403, RequestID: R, " +
				"api error AccessDenied: Access Denied",
			want: ReasonS3Auth,
		},
		"S3 密钥不存在": {
			raw: "operation error S3: HeadObject, https response error StatusCode: 403, RequestID: R, " +
				"api error InvalidAccessKeyId: The AWS Access Key Id you provided does not exist",
			want: ReasonS3Auth,
		},
		"WebDAV 密码错误":   {raw: `写入对象 "probe": 401 Unauthorized`, want: ReasonAuth},
		"OneDrive 令牌无效": {raw: "InvalidAuthenticationToken: Access token has expired", want: ReasonAuth},
		"OneDrive 无权限":  {raw: "accessDenied: Access denied", want: ReasonForbidden},
		"刷新令牌被拒": {
			raw:  `couldn't fetch token: invalid_grant: maybe token expired? - try refreshing`,
			want: ReasonGrantRevoked,
		},
		"自建应用凭证错误": {
			raw:  "couldn't fetch token: invalid_client: if you're using your own client id/secret",
			want: ReasonAppCredentials,
		},
		"无法归类": {raw: "something odd", want: ReasonUnclassified},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Explain(errors.New(tc.raw)).Reason; got != tc.want {
				t.Errorf("归类 = %s, 期望 %s", got, tc.want)
			}
		})
	}
}

// TestExplainDetailIsRootCause 验证原文取错误链最内层、抹除凭证并截断,哨兵与配置错误按类型归类。
func TestExplainDetailIsRootCause(t *testing.T) {
	root := errors.New("dial tcp 10.0.0.1:443: connect: connection refused (key=SECRETVALUE)")
	wrapped := fmt.Errorf("写入对象 %q: %w", "k", fmt.Errorf("上传: %w", root))
	f := Explain(wrapped, "SECRETVALUE", "")
	if f.Reason != ReasonConnect || f.Detail != "dial tcp 10.0.0.1:443: connect: connection refused (key=***)" {
		t.Fatalf("Explain = %+v", f)
	}
	long := Explain(errors.New(string(make([]rune, failureDetailMaxLen+10))))
	if r := []rune(long.Detail); len(r) != failureDetailMaxLen || r[len(r)-1] != '…' {
		t.Errorf("截断后长度 = %d、末字符 %q, 期望 %d 个字符且以省略号结尾", len(r), r[len(r)-1], failureDetailMaxLen)
	}
	if got := Explain(fmt.Errorf("探测: %w", failover.ErrProbeMismatch)).Reason; got != ReasonProbeMismatch {
		t.Errorf("读回不一致归类 = %s", got)
	}
	unreadable := configError{err: errors.New("cipher: message authentication failed")}
	if got := Explain(fmt.Errorf("装载: %w", unreadable)).Reason; got != ReasonConfigUnreadable {
		t.Errorf("配置解密失败归类 = %s", got)
	}
	if got := Explain(fmt.Errorf("探测: %w", context.DeadlineExceeded)).Reason; got != ReasonTimeout {
		t.Errorf("超时归类 = %s", got)
	}
}

// TestFailureMessage 验证中文说明:归类说明加原文,无法归类时只有原文,环节前缀在最前。
func TestFailureMessage(t *testing.T) {
	cases := []struct {
		f    LastFailure
		want string
	}{
		{LastFailure{StageProbe, Failure{ReasonTimeout, "i/o timeout"}}, "连接超时:检查端点地址与网络连通性(i/o timeout)"},
		{LastFailure{StageLoad, Failure{ReasonUnclassified, "boom"}}, "装载失败:boom"},
		{LastFailure{StageProbe, Failure{ReasonNoSource, ""}}, "没有可读的源副本"},
	}
	for _, tc := range cases {
		if got := tc.f.Message(); got != tc.want {
			t.Errorf("Message(%+v) = %q, 期望 %q", tc.f, got, tc.want)
		}
	}
}
