package taxcalc

import "math/big"

// Tx 为单笔待核算交易。
type Tx struct {
	Jurisdiction string // 辖区
	TaxSystem    string // 税制
	Category     string // 商品类别
	Date         Date   // 交易日期
	Amount       int64  // 金额，非负整数最小货币单位
}

// rateDivisor 为 Rate 换算为比例的除数：Rate(130000) / 1e6 = 0.13。
const rateDivisor = rateScale * 100

// Compute 按知悉序号上界 maxSeq 核算单笔交易。
// maxSeq 表示核算时掌握的信息：只考虑知悉序号不大于 maxSeq 的变更。
// 后续登记的新变更不会污染本次结论。
func (r *Registry) Compute(tx Tx, maxSeq uint64) Result {
	return computeOn(r.snapshot(), tx, maxSeq)
}

// ComputeBatch 按同一知悉序号上界批量核算历史交易。
// 整个批次基于同一快照，结果与逐笔调用 Compute 一致。
func (r *Registry) ComputeBatch(txs []Tx, maxSeq uint64) []Result {
	snap := r.snapshot()
	results := make([]Result, len(txs))
	for i, tx := range txs {
		results[i] = computeOn(snap, tx, maxSeq)
	}
	return results
}

// computeOn 在快照上解析适用范围并核算。
// 先确定商品专属范围：取知悉序号不大于 maxSeq 且覆盖交易日期的
// 最高知悉序号变更；应税或免税直接生效（优先于全部商品规定），
// 撤销或没有适用变更时再判断全部商品范围。
// 撤销不恢复该范围更早登记的税率；全部商品范围也被撤销或
// 没有适用变更时，结论为无规则。
func computeOn(changes []Change, tx Tx, maxSeq uint64) Result {
	if specific, ok := lookup(changes, tx, maxSeq, tx.Category); ok {
		if specific.Kind != KindRevoke {
			return apply(specific, tx.Amount)
		}
	}
	if general, ok := lookup(changes, tx, maxSeq, ""); ok {
		if general.Kind != KindRevoke {
			return apply(general, tx.Amount)
		}
	}
	return Result{Conclusion: ConclusionNoRule}
}

// lookup 在给定适用范围（category 为空串表示全部商品）内，
// 返回匹配辖区与税制、知悉序号不超过 maxSeq、生效区间覆盖
// 交易日期、且知悉序号最大的那条变更。
func lookup(changes []Change, tx Tx, maxSeq uint64, category string) (Change, bool) {
	var best Change
	bestSeq := uint64(0)
	found := false
	for i, c := range changes {
		seq := uint64(i + 1)
		if seq > maxSeq {
			break
		}
		if c.Category != category ||
			c.Jurisdiction != tx.Jurisdiction ||
			c.TaxSystem != tx.TaxSystem ||
			!c.covers(tx.Date) {
			continue
		}
		if !found || seq > bestSeq {
			best, bestSeq, found = c, seq, true
		}
	}
	return best, found
}

// apply 将一条应税或免税变更应用到交易金额。
func apply(c Change, amount int64) Result {
	if c.Kind == KindExempt {
		return Result{Conclusion: ConclusionExempt}
	}
	return Result{
		Conclusion: ConclusionTaxable,
		Rate:       c.Rate,
		Tax:        roundTax(amount, c.Rate),
	}
}

// roundTax 以整数运算计算税额并四舍五入到整数最小货币单位，
// 恰好半单位时向上取整，不发生浮点误差。
// tax = amount * rate / rateDivisor，余数达到除数一半即进位。
func roundTax(amount int64, rate Rate) int64 {
	num := new(big.Int).Mul(big.NewInt(amount), big.NewInt(int64(rate)))
	half := big.NewInt(rateDivisor / 2)
	num.Add(num, half)
	num.Quo(num, big.NewInt(rateDivisor))
	return num.Int64()
}
