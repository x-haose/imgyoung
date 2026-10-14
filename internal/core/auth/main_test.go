package auth

import "testing"

// TestMain 把 argon2id 开销调到最低档:用例只关心哈希与限速的逻辑,不关心抗暴力破解的强度,
// 出厂参数会让每次登录与改密用例各付几十毫秒,竞态检测下整包要跑一分钟以上。
func TestMain(m *testing.M) {
	hashCost = argonCost{memoryKiB: 256, iterations: 1, lanes: 1}
	m.Run()
}
