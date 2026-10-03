package indtax

import (
	"math/big"
	"time"
)

// rateScale 是税率存储单位与百分比的换算基数：rate / rateScale = 百分比数值。
// 例如 Rate 为 130000 表示 13.0000%，税额 = 金额 * Rate / (rateScale * 100)。
const rateDivisor = 1_000_000 // rateScale(1e4) * 100

// pick 在给定适用范围内，选出知悉序号不大于 maxSeq 且生效区间覆盖 date 的
// 知悉序号最大的变更。changes 按知悉序号升序排列，最后一个匹配者即所求。
func pick(changes []Change, maxSeq uint64, jurisdiction, taxSystem, category string, date time.Time) (Change, bool) {
	var best Change
	found := false
	for i, c := range changes {
		seq := uint64(i) + 1
		if seq > maxSeq {
			break
		}
		if c.Jurisdiction != jurisdiction || c.TaxSystem != taxSystem || c.Category != category {
			continue
		}
		if !c.covers(date) {
			continue
		}
		best = c
		found = true
	}
	return best, found
}

// applyChange 按变更内容计算税额。amount 与 rate 均非负，
// 乘积加半个除数后整除，实现恰好半单位向上取整，无浮点误差。
func applyChange(c Change, amount int64) (Result, error) {
	switch c.Kind {
	case KindTaxable:
		product := new(big.Int).Mul(big.NewInt(amount), big.NewInt(c.Rate))
		product.Add(product, big.NewInt(rateDivisor/2))
		tax := product.Div(product, big.NewInt(rateDivisor))
		if !tax.IsInt64() {
			return Result{}, ErrTaxOverflow
		}
		return Result{Status: StatusTaxable, Rate: c.Rate, Tax: tax.Int64()}, nil
	case KindExempt:
		return Result{Status: StatusExempt, Tax: 0}, nil
	default:
		return Result{}, ErrInvalidKind
	}
}

// assess 在快照上核算单笔交易。
func assess(changes []Change, t Transaction) (Result, error) {
	if t.Amount < 0 {
		return Result{}, ErrNegativeAmount
	}
	// 先确定商品专属范围：存在适用变更且非撤销时直接采用，
	// 专属应税或免税均优先于全部商品规定，不受登记先后影响。
	if t.Category != "" {
		if c, ok := pick(changes, t.MaxSeq, t.Jurisdiction, t.TaxSystem, t.Category, t.Date); ok && c.Kind != KindRevoke {
			return applyChange(c, t.Amount)
		}
	}
	// 专属范围没有适用变更或结果为撤销时，再判断全部商品范围。
	// 撤销只让本范围失去适用资格，不能恢复更早登记的税率。
	if c, ok := pick(changes, t.MaxSeq, t.Jurisdiction, t.TaxSystem, "", t.Date); ok && c.Kind != KindRevoke {
		return applyChange(c, t.Amount)
	}
	// 全部商品范围也被撤销或没有适用变更时，结论为无规则。
	return Result{Status: StatusNoRule}, nil
}

// Assess 核算单笔交易。
func (l *Ledger) Assess(t Transaction) (Result, error) {
	changes, _ := l.snapshot()
	return assess(changes, t)
}

// AssessBatch 批量历史核算，每笔交易使用各自的交易日期与知悉序号上界。
// 返回与输入等长的结果切片；任一交易参数非法时整体返回错误。
func (l *Ledger) AssessBatch(txns []Transaction) ([]Result, error) {
	changes, _ := l.snapshot()
	results := make([]Result, len(txns))
	for i, t := range txns {
		r, err := assess(changes, t)
		if err != nil {
			return nil, err
		}
		results[i] = r
	}
	return results, nil
}
