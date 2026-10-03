package taxcalc

import "sync"

// Registry 保存已登记税则变更的不可变历史。
// 每次登记获得全局严格递增的知悉序号（从 1 开始）。
// 已登记内容不能修改。Registry 可安全并发使用。
type Registry struct {
	mu      sync.RWMutex
	changes []Change
}

// NewRegistry 创建空的税则注册表。
func NewRegistry() *Registry { return &Registry{} }

// Register 登记一条已颁布的税则变更，返回其知悉序号。
// 传入的 Change 会被深拷贝保存，调用方之后修改原值不影响已登记内容。
func (r *Registry) Register(c Change) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := c
	if c.To != nil {
		to := *c.To
		stored.To = &to
	}
	r.changes = append(r.changes, stored)
	return uint64(len(r.changes))
}

// Len 返回当前已登记的变更数量，即最大知悉序号。
func (r *Registry) Len() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return uint64(len(r.changes))
}

// snapshot 返回已登记变更的只读快照。
// 快照在返回后不受后续登记影响，保证一次批量核算使用一致的视图。
func (r *Registry) snapshot() []Change {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Change, len(r.changes))
	copy(out, r.changes)
	return out
}
