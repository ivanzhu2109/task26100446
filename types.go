// Package taxcalc 提供可追溯的间接税核算能力。
// 税则及其不可变历史保存在进程内，由上层直接调用。
package taxcalc

import (
	"fmt"
	"strconv"
	"strings"
)

// Date 为不含时区的历法日期，可比较。
type Date struct {
	Year  int
	Month int
	Day   int
}

// Before 报告 d 是否严格早于 other。
func (d Date) Before(other Date) bool {
	if d.Year != other.Year {
		return d.Year < other.Year
	}
	if d.Month != other.Month {
		return d.Month < other.Month
	}
	return d.Day < other.Day
}

// Equal 报告 d 是否与 other 相同。
func (d Date) Equal(other Date) bool {
	return d.Year == other.Year && d.Month == other.Month && d.Day == other.Day
}

// Rate 为定点税率，保留四位小数，单位为万分之一百分点。
// 例如 13.0000% 表示为 130000。
type Rate int64

// rateScale 为 Rate 的缩放因子：1 个百分点 = 10000。
const rateScale = 10000

// RateFromString 解析形如 "13.0000" 的百分数字符串。
// 小数部分最多四位，超出部分将被拒绝以避免精度损失。
func RateFromString(s string) (Rate, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("taxcalc: empty rate string")
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		return 0, fmt.Errorf("taxcalc: negative rate %q", s)
	}
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	intVal, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("taxcalc: invalid rate %q: %w", s, err)
	}
	var fracVal int64
	if hasFrac {
		if len(fracPart) > 4 {
			return 0, fmt.Errorf("taxcalc: rate %q has more than 4 decimal places", s)
		}
		fracPart = fracPart + strings.Repeat("0", 4-len(fracPart))
		fracVal, err = strconv.ParseInt(fracPart, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("taxcalc: invalid rate %q: %w", s, err)
		}
	}
	return Rate(intVal*rateScale + fracVal), nil
}

// ChangeKind 为变更内容类型。
type ChangeKind int

const (
	// KindTaxable 应税，附带税率。
	KindTaxable ChangeKind = iota
	// KindExempt 免税。
	KindExempt
	// KindRevoke 撤销，使该范围在生效区间内失去适用资格。
	KindRevoke
)

// Change 为一条已颁布的税则变更，登记后不可修改。
type Change struct {
	Jurisdiction string     // 辖区
	TaxSystem    string     // 税制
	Category     string     // 商品类别；空字符串表示全部商品
	Kind         ChangeKind // 变更内容
	Rate         Rate       // 应税税率，其余类型忽略
	From         Date       // 生效起日（含）
	To           *Date      // 生效止日（不含）；nil 表示无限延续
}

// covers 报告生效区间是否覆盖交易日期 d：含起日、不含止日。
func (c Change) covers(d Date) bool {
	if d.Before(c.From) {
		return false
	}
	if c.To != nil && !d.Before(*c.To) {
		return false
	}
	return true
}

// Conclusion 为核算结论类型。
type Conclusion int

const (
	// ConclusionTaxable 应税。
	ConclusionTaxable Conclusion = iota
	// ConclusionExempt 免税，税额为 0。
	ConclusionExempt
	// ConclusionNoRule 无规则，不能冒充零税额。
	ConclusionNoRule
)

// Result 为单笔交易的核算结果。
type Result struct {
	Conclusion Conclusion // 结论
	Rate       Rate       // 适用税率（仅应税时有效）
	Tax        int64      // 税额，四舍五入到整数最小货币单位
}
