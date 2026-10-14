package channel

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// oauthSessionTTL 授权会话存活时长;用户在授权页停留过久即作废重来。
const oauthSessionTTL = 10 * time.Minute

// ErrOAuthSessionNotFound 授权会话不存在或已过期。
var ErrOAuthSessionNotFound = fmt.Errorf("%w: 授权会话不存在或已过期", ErrInvalidChannel)

// oauthSession 一次进行中的授权。
type oauthSession struct {
	driver      string            // rclone 后端名
	params      map[string]string // 后端参数,含自建应用凭证
	redirectURI string            // 发起时使用的回调地址,换取令牌时必须一致
	createdAt   time.Time         // 创建时刻,用于过期判定
	token       string            // 换取成功的令牌 JSON;未完成为空
	failure     string            // 换取失败的原因;成功为空
}

// OAuthSessions 进行中的授权会话表。
// @Description: 授权码流要求发起与回调之间保有 client_secret 与后端参数,而 state 会
// 经授权服务器往返、不可承载密钥,故只能服务端暂存。会话是短命的进程内状态,重启丢失
// 只意味着用户重新点一次授权,不值得落库。
type OAuthSessions struct {
	mu       sync.Mutex
	sessions map[string]*oauthSession
}

// NewOAuthSessions 创建授权会话表。
// @return 会话表
func NewOAuthSessions() *OAuthSessions {
	return &OAuthSessions{sessions: map[string]*oauthSession{}}
}

// Start 登记一次授权并返回其 state。
// @param driver rclone 后端名
// @param params 后端参数,含自建应用凭证
// @param redirectURI 回调地址
// @return 一次性 state
// @return 随机源不可用时的错误
func (s *OAuthSessions) Start(driver string, params map[string]string, redirectURI string) (string, error) {
	state, err := randomState()
	if err != nil {
		return "", err
	}
	cloned := make(map[string]string, len(params))
	for k, v := range params {
		cloned[k] = v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked()
	s.sessions[state] = &oauthSession{
		driver: driver, params: cloned, redirectURI: redirectURI, createdAt: time.Now(),
	}
	return state, nil
}

// PendingAuth 一次待换取令牌的授权的发起信息。
type PendingAuth struct {
	Driver      string            // rclone 后端名
	Params      map[string]string // 后端参数,含自建应用凭证
	RedirectURI string            // 发起时使用的回调地址,换取令牌时必须一致
}

// Pending 取出待换取令牌的会话信息。
// @param state 一次性 state
// @return 发起信息
// @return ErrOAuthSessionNotFound
func (s *OAuthSessions) Pending(state string) (PendingAuth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[state]
	if !ok || expired(sess) {
		return PendingAuth{}, ErrOAuthSessionNotFound
	}
	return PendingAuth{Driver: sess.driver, Params: sess.params, RedirectURI: sess.redirectURI}, nil
}

// Complete 记录换取结果。
// @param state 一次性 state
// @param token 换取到的令牌 JSON;失败时为空
// @param failure 失败原因;成功时为空
// @return ErrOAuthSessionNotFound
func (s *OAuthSessions) Complete(state, token, failure string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[state]
	if !ok || expired(sess) {
		return ErrOAuthSessionNotFound
	}
	sess.token, sess.failure = token, failure
	return nil
}

// Take 取走授权结果,取走即销毁会话。
// @Description: 令牌只允许被领取一次,避免同一 state 被重放领第二次。
// @param state 一次性 state
// @return token 令牌 JSON;尚未完成时为空
// @return failure 失败原因;无失败时为空
// @return err ErrOAuthSessionNotFound
func (s *OAuthSessions) Take(state string) (token, failure string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[state]
	if !ok || expired(sess) {
		return "", "", ErrOAuthSessionNotFound
	}
	if sess.token == "" && sess.failure == "" {
		return "", "", nil
	}
	delete(s.sessions, state)
	return sess.token, sess.failure, nil
}

// expired 判定会话是否已过期。
// @param sess 会话
// @return 是否过期
func expired(sess *oauthSession) bool {
	return time.Since(sess.createdAt) > oauthSessionTTL
}

// evictExpiredLocked 清理过期会话;调用方须持锁。
func (s *OAuthSessions) evictExpiredLocked() {
	for state, sess := range s.sessions {
		if expired(sess) {
			delete(s.sessions, state)
		}
	}
}

// randomState 生成一次性 state。
// @return state
// @return 随机源不可用时的错误
func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
