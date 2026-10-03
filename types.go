// Package indtax 提供可追溯的间接税核算能力。
//
// 税则及其不可变历史保存在进程内。每笔核算指定交易日期与知悉序号上界，
// 后者表示申报当时掌握的信息，新增追溯更正不会污染历史结论。
package indtax

import (
	"errors"
	"time"
)

// Kind 表示税则变更的内容类型。
type Kind int

const (
	// KindTaxable 应税，附带税率。
	KindTaxable Kind = iota
	// KindExempt 免税。
	KindExempt
	// KindRevoke 撤销：仅使本范围在生效区间内失去适用资格。
	KindRevoke
)

// Change 是一条已颁布的税则变更，登记后不可修改。
type Change struct {
	Jurisdiction  string    // 辖区
	TaxSystem     string    // 税制
	Category      string    // 商品类别；空字符串表示全部商品
	Kind          Kind      // 变更类型
	Rate          int64     // 税率，单位 1e-4（13.0000 表示 13%，存储为 130000），仅 KindTaxable 有效
	EffectiveFrom time.Time // 生效起日（含）
	EffectiveTo   time.Time // 生效止日（不含）；零值表示无限延续
}

// 参数错误。
var (
	ErrInvalidKind     = errors.New("indtax: 未知的变更类型")
	ErrNegativeRate    = errors.New("indtax: 税率不能为负")
	ErrUnexpectedRate  = errors.New("indtax: 免税或撤销变更不得附带税率")
	ErrInvalidInterval = errors.New("indtax: 生效止日必须晚于起日")
	ErrNegativeAmount  = errors.New("indtax: 金额不能为负")
	ErrTaxOverflow     = errors.New("indtax: 税额超出 int64 范围")
)

// validate 校验变更是否合法。
func (c Change) validate() error {
	switch c.Kind {
	case KindTaxable:
		if c.Rate < 0 {
			return ErrNegativeRate
		}
	case KindExempt, KindRevoke:
		if c.Rate != 0 {
			return ErrUnexpectedRate
		}
	default:
		return ErrInvalidKind
	}
	if !c.EffectiveTo.IsZero() && !c.EffectiveTo.After(c.EffectiveFrom) {
		return ErrInvalidInterval
	}
	return nil
}

// covers 判断生效区间是否覆盖给定日期：含起日、不含止日，止日为零值表示无限延续。
func (c Change) covers(date time.Time) bool {
	if date.Before(c.EffectiveFrom) {
		return false
	}
	if !c.EffectiveTo.IsZero() && !date.Before(c.EffectiveTo) {
		return false
	}
	return true
}

// Status 表示核算结论。
type Status int

const (
	// StatusTaxable 应税。
	StatusTaxable Status = iota
	// StatusExempt 免税，税额为 0。
	StatusExempt
	// StatusNoRule 无规则，不能冒充零税额。
	StatusNoRule
)

// Result 是单笔核算结果。
type Result struct {
	Status Status
	Rate   int64 // 适用税率（1e-4 单位），仅 StatusTaxable 有效
	Tax    int64 // 税额，最小货币单位
}

// Transaction 是一笔待核算交易。
type Transaction struct {
	Jurisdiction string
	TaxSystem    string
	Category     string
	Date         time.Time // 交易日期
	MaxSeq       uint64    // 知悉序号上界：当时掌握的信息
	Amount       int64     // 金额，非负整数最小货币单位
}
