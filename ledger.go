package indtax

import "sync"

// Ledger 保存税则及其不可变历史，全部数据在进程内。
// 已登记的变更不可修改，知悉序号全局严格递增。
type Ledger struct {
	mu      sync.RWMutex
	changes []Change // 下标 i 对应知悉序号 i+1
}

// NewLedger 创建空账本。
func NewLedger() *Ledger {
	return &Ledger{}
}

// Register 登记一条已颁布税则变更，返回全局严格递增的知悉序号（从 1 开始）。
// 登记内容不可修改；登记先后与生效日期先后没有必然关系。
func (l *Ledger) Register(c Change) (uint64, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.changes = append(l.changes, c)
	return uint64(len(l.changes)), nil
}

// snapshot 返回已登记变更的只读副本及当前最大知悉序号。
func (l *Ledger) snapshot() ([]Change, uint64) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Change, len(l.changes))
	copy(out, l.changes)
	return out, uint64(len(l.changes))
}
