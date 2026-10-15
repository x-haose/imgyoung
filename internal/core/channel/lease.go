package channel

import "sync"

// writeLock 返回渠道的写入租约锁,没有则创建。
// @param channelID 渠道主键
// @return 锁
func (m *Manager) writeLock(channelID int64) *sync.RWMutex {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if m.writeLocks == nil {
		m.writeLocks = map[int64]*sync.RWMutex{}
	}
	l, ok := m.writeLocks[channelID]
	if !ok {
		l = &sync.RWMutex{}
		m.writeLocks[channelID] = l
	}
	return l
}

// HoldWrites 为往渠道写对象占用渠道的写入租约:上传、补齐、元数据库快照、深探测都在租约内写入并落下记录。
// @Description: 持读锁后再确认渠道在役:渠道一旦标为清理中,之后的写入在此被拒;此前已占用的写入,排空开工前
// 的 AwaitWrites 会等它释放,租约内写下的对象与记录都会被排空看到,不会漏删。多个写入可同时占用。
// @param channelID 渠道主键
// @return release 写完并落下记录后调用;ok 为 false 时为 nil
// @return ok 渠道在役为 true;已标为清理中或不在管理器中为 false,此时不得写入
func (m *Manager) HoldWrites(channelID int64) (release func(), ok bool) {
	l := m.writeLock(channelID)
	l.RLock()
	if h, found := m.Get(channelID); !found || h.PendingDelete {
		l.RUnlock()
		return nil, false
	}
	return l.RUnlock, true
}

// AwaitWrites 等渠道上在途的写入租约全部释放;排空在渠道标为清理中之后、列举对象与记录之前调用。
// @Description: 标记之后新的 HoldWrites 一律被拒,所以等到的这一刻起渠道上不再有本服务的新写入。
// @param channelID 渠道主键,须已标为清理中
func (m *Manager) AwaitWrites(channelID int64) {
	l := m.writeLock(channelID)
	l.Lock()
	defer l.Unlock()
}
