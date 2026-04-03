package mailer

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// VerifyCodeStore 验证码内存存储。
type VerifyCodeStore struct {
	mu    sync.RWMutex
	codes map[string]codeEntry
}

type codeEntry struct {
	code      string
	expiresAt time.Time
}

// NewVerifyCodeStore 创建验证码存储。
func NewVerifyCodeStore() *VerifyCodeStore {
	s := &VerifyCodeStore{codes: make(map[string]codeEntry)}
	// 后台清理过期条目
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			s.cleanup()
		}
	}()
	return s
}

// Generate 为指定邮箱生成 6 位验证码，有效期 10 分钟。
func (s *VerifyCodeStore) Generate(email string) string {
	code := randomCode()
	s.mu.Lock()
	s.codes[email] = codeEntry{
		code:      code,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
	s.mu.Unlock()
	return code
}

// Verify 校验验证码，成功后自动删除。
func (s *VerifyCodeStore) Verify(email, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.codes[email]
	if !ok {
		return false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.codes, email)
		return false
	}
	if entry.code != code {
		return false
	}
	delete(s.codes, email)
	return true
}

func (s *VerifyCodeStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.codes {
		if now.After(v.expiresAt) {
			delete(s.codes, k)
		}
	}
}

func randomCode() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%06d", int(b[0])<<16|int(b[1])<<8|int(b[2]))[:6]
}
