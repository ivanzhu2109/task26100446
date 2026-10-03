package indtax

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func mustRegister(t *testing.T, l *Ledger, c Change) uint64 {
	t.Helper()
	seq, err := l.Register(c)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	return seq
}

func mustAssess(t *testing.T, l *Ledger, txn Transaction) Result {
	t.Helper()
	r, err := l.Assess(txn)
	if err != nil {
		t.Fatalf("Assess failed: %v", err)
	}
	return r
}

// 追溯更正与原区间部分重叠：只有更正已被知悉且日期被覆盖时结果才改变。
func TestRetroactiveCorrectionPartialOverlap(t *testing.T) {
	l := NewLedger()
	// 原始规定：全部商品 10%，2024 全年。
	seq1 := mustRegister(t, l, Change{
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "",
		Kind: KindTaxable, Rate: 100000, // 10.0000%
		EffectiveFrom: date(2024, 1, 1), EffectiveTo: date(2025, 1, 1),
	})
	// 追溯更正：全部商品 20%，仅覆盖 2024-06-01 至 2024-09-01。
	seq2 := mustRegister(t, l, Change{
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "",
		Kind: KindTaxable, Rate: 200000, // 20.0000%
		EffectiveFrom: date(2024, 6, 1), EffectiveTo: date(2024, 9, 1),
	})
	if !(seq1 < seq2) {
		t.Fatalf("seq must be strictly increasing: %d, %d", seq1, seq2)
	}
	txn := func(d time.Time, maxSeq uint64) Transaction {
		return Transaction{Jurisdiction: "CN", TaxSystem: "VAT", Category: "book",
			Date: d, MaxSeq: maxSeq, Amount: 10000}
	}
	cases := []struct {
		name   string
		d      time.Time
		maxSeq uint64
		want   Result
	}{
		{"更正未知悉_区间内", date(2024, 7, 1), seq1, Result{Status: StatusTaxable, Rate: 100000, Tax: 1000}},
		{"更正已知悉_区间内", date(2024, 7, 1), seq2, Result{Status: StatusTaxable, Rate: 200000, Tax: 2000}},
		{"更正已知悉_区间外之前", date(2024, 5, 31), seq2, Result{Status: StatusTaxable, Rate: 100000, Tax: 1000}},
		{"更正已知悉_区间外之后", date(2024, 9, 1), seq2, Result{Status: StatusTaxable, Rate: 100000, Tax: 1000}},
		{"恰落在更正起日_含", date(2024, 6, 1), seq2, Result{Status: StatusTaxable, Rate: 200000, Tax: 2000}},
		{"恰落在更正止日_不含", date(2024, 9, 1), seq2, Result{Status: StatusTaxable, Rate: 100000, Tax: 1000}},
		{"恰落在原规定起日_含", date(2024, 1, 1), seq2, Result{Status: StatusTaxable, Rate: 100000, Tax: 1000}},
		{"恰落在原规定止日_不含", date(2025, 1, 1), seq2, Result{Status: StatusNoRule}},
		{"早于任何生效区间", date(2023, 12, 31), seq2, Result{Status: StatusNoRule}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustAssess(t, l, txn(tc.d, tc.maxSeq)); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// 专属免税遮蔽通用税率 → 专属撤销回退通用规则 → 通用也撤销变为无规则，
// 全程不得复活旧专属税率。
func TestScopeFallbackChain(t *testing.T) {
	l := NewLedger()
	mustRegister(t, l, Change{ // seq1 通用 13%
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: 130000, EffectiveFrom: date(2024, 1, 1),
	})
	mustRegister(t, l, Change{ // seq2 旧专属税率 5%
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "food",
		Kind: KindTaxable, Rate: 50000, EffectiveFrom: date(2024, 1, 1),
	})
	mustRegister(t, l, Change{ // seq3 专属免税，自 3 月起
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "food",
		Kind: KindExempt, EffectiveFrom: date(2024, 3, 1),
	})
	mustRegister(t, l, Change{ // seq4 专属撤销，自 6 月起
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "food",
		Kind: KindRevoke, EffectiveFrom: date(2024, 6, 1),
	})
	mustRegister(t, l, Change{ // seq5 通用撤销，自 9 月起
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindRevoke, EffectiveFrom: date(2024, 9, 1),
	})
	txn := func(d time.Time) Transaction {
		return Transaction{Jurisdiction: "CN", TaxSystem: "VAT", Category: "food",
			Date: d, MaxSeq: 5, Amount: 10000}
	}
	cases := []struct {
		name string
		d    time.Time
		want Result
	}{
		{"旧专属税率生效", date(2024, 2, 1), Result{Status: StatusTaxable, Rate: 50000, Tax: 500}},
		{"专属免税遮蔽通用税率", date(2024, 4, 1), Result{Status: StatusExempt, Tax: 0}},
		{"专属撤销回退通用规则_不复活旧专属税率", date(2024, 7, 1), Result{Status: StatusTaxable, Rate: 130000, Tax: 1300}},
		{"通用也撤销后无规则", date(2024, 10, 1), Result{Status: StatusNoRule}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustAssess(t, l, txn(tc.d)); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// 撤销只影响自身生效区间，区间外仍按此前变更判断。
func TestRevokeLimitedToOwnInterval(t *testing.T) {
	l := NewLedger()
	mustRegister(t, l, Change{ // seq1 通用 10%，全年
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: 100000, EffectiveFrom: date(2024, 1, 1),
	})
	mustRegister(t, l, Change{ // seq2 通用撤销，仅 6 月
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindRevoke, EffectiveFrom: date(2024, 6, 1), EffectiveTo: date(2024, 7, 1),
	})
	txn := func(d time.Time) Transaction {
		return Transaction{Jurisdiction: "CN", TaxSystem: "VAT", Category: "book",
			Date: d, MaxSeq: 2, Amount: 10000}
	}
	if got := mustAssess(t, l, txn(date(2024, 6, 15))); got.Status != StatusNoRule {
		t.Errorf("撤销区间内应为无规则, got %+v", got)
	}
	want := Result{Status: StatusTaxable, Rate: 100000, Tax: 1000}
	if got := mustAssess(t, l, txn(date(2024, 7, 1))); got != want {
		t.Errorf("撤销区间外应恢复此前变更, got %+v, want %+v", got, want)
	}
}

// 逐笔四舍五入，恰好半单位向上取整，无浮点误差。
func TestRoundingHalfUp(t *testing.T) {
	l := NewLedger()
	mustRegister(t, l, Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: 10000, // 1.0000%
		EffectiveFrom: date(2024, 1, 1),
	})
	txn := func(amount int64) Transaction {
		return Transaction{Jurisdiction: "CN", TaxSystem: "VAT", Category: "book",
			Date: date(2024, 2, 1), MaxSeq: 1, Amount: amount}
	}
	cases := []struct {
		amount int64
		want   int64
	}{
		{49, 0},  // 0.49 → 0
		{50, 1},  // 恰好 0.5 → 1
		{150, 2}, // 1.5 → 2
		{151, 2}, // 1.51 → 2
		{100, 1}, // 1.0 → 1
		{0, 0},   // 零金额
	}
	for _, tc := range cases {
		if got := mustAssess(t, l, txn(tc.amount)); got.Tax != tc.want {
			t.Errorf("amount %d: got tax %d, want %d", tc.amount, got.Tax, tc.want)
		}
	}
}

// 零税率应税与免税均不触发回退；无规则不能冒充零税额。
func TestZeroRateAndExemptNoFallback(t *testing.T) {
	l := NewLedger()
	mustRegister(t, l, Change{ // seq1 通用 13%
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: 130000, EffectiveFrom: date(2024, 1, 1),
	})
	mustRegister(t, l, Change{ // seq2 专属零税率
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "export",
		Kind: KindTaxable, Rate: 0, EffectiveFrom: date(2024, 1, 1),
	})
	mustRegister(t, l, Change{ // seq3 专属免税
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "med",
		Kind: KindExempt, EffectiveFrom: date(2024, 1, 1),
	})
	txn := func(cat string) Transaction {
		return Transaction{Jurisdiction: "CN", TaxSystem: "VAT", Category: cat,
			Date: date(2024, 2, 1), MaxSeq: 3, Amount: 10000}
	}
	if got := mustAssess(t, l, txn("export")); got != (Result{Status: StatusTaxable, Rate: 0, Tax: 0}) {
		t.Errorf("零税率应税不应回退, got %+v", got)
	}
	if got := mustAssess(t, l, txn("med")); got != (Result{Status: StatusExempt, Tax: 0}) {
		t.Errorf("免税不应回退, got %+v", got)
	}
	if got := mustAssess(t, l, txn("other")); got != (Result{Status: StatusTaxable, Rate: 130000, Tax: 1300}) {
		t.Errorf("无专属规定应适用通用规则, got %+v", got)
	}
}

// 新增追溯更正后，按旧知悉序号核算结果完全不变；批量核算各笔独立。
func TestHistoricalReproducibilityAndBatch(t *testing.T) {
	l := NewLedger()
	seq1 := mustRegister(t, l, Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: 100000, EffectiveFrom: date(2024, 1, 1),
	})
	txn := Transaction{Jurisdiction: "CN", TaxSystem: "VAT", Category: "book",
		Date: date(2024, 7, 1), MaxSeq: seq1, Amount: 12345}
	before, err := l.AssessBatch([]Transaction{txn})
	if err != nil {
		t.Fatalf("AssessBatch failed: %v", err)
	}
	// 登记追溯更正与无关辖区规定。
	seq2 := mustRegister(t, l, Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: 200000, EffectiveFrom: date(2024, 6, 1), EffectiveTo: date(2024, 8, 1),
	})
	mustRegister(t, l, Change{
		Jurisdiction: "US", TaxSystem: "SALES",
		Kind: KindTaxable, Rate: 70000, EffectiveFrom: date(2024, 1, 1),
	})
	after, err := l.AssessBatch([]Transaction{txn})
	if err != nil {
		t.Fatalf("AssessBatch failed: %v", err)
	}
	if before[0] != after[0] {
		t.Errorf("旧知悉序号结果被污染: before %+v, after %+v", before[0], after[0])
	}
	// 新上界看到更正；不同辖区互不回退。
	txnNow := txn
	txnNow.MaxSeq = seq2
	if got := mustAssess(t, l, txnNow); got != (Result{Status: StatusTaxable, Rate: 200000, Tax: 2469}) {
		t.Errorf("新知悉序号应看到更正, got %+v", got)
	}
	txnUS := txn
	txnUS.Jurisdiction = "US"
	txnUS.TaxSystem = "SALES"
	txnUS.MaxSeq = 3
	if got := mustAssess(t, l, txnUS); got != (Result{Status: StatusTaxable, Rate: 70000, Tax: 864}) {
		t.Errorf("辖区与税制应隔离, got %+v", got)
	}
	txnUnknown := txn
	txnUnknown.Jurisdiction = "JP"
	txnUnknown.MaxSeq = 3
	if got := mustAssess(t, l, txnUnknown); got.Status != StatusNoRule {
		t.Errorf("未知辖区应为无规则, got %+v", got)
	}
}

// 参数校验。
func TestValidation(t *testing.T) {
	l := NewLedger()
	if _, err := l.Register(Change{Jurisdiction: "CN", TaxSystem: "VAT", Kind: Kind(99),
		EffectiveFrom: date(2024, 1, 1)}); err == nil {
		t.Error("未知变更类型应报错")
	}
	if _, err := l.Register(Change{Jurisdiction: "CN", TaxSystem: "VAT", Kind: KindTaxable, Rate: -1,
		EffectiveFrom: date(2024, 1, 1)}); err == nil {
		t.Error("负税率应报错")
	}
	if _, err := l.Register(Change{Jurisdiction: "CN", TaxSystem: "VAT", Kind: KindExempt, Rate: 1,
		EffectiveFrom: date(2024, 1, 1)}); err == nil {
		t.Error("免税附带税率应报错")
	}
	if _, err := l.Register(Change{Jurisdiction: "CN", TaxSystem: "VAT", Kind: KindTaxable, Rate: 1,
		EffectiveFrom: date(2024, 6, 1), EffectiveTo: date(2024, 6, 1)}); err == nil {
		t.Error("止日不晚于起日应报错")
	}
	if _, err := l.Assess(Transaction{Jurisdiction: "CN", TaxSystem: "VAT", Date: date(2024, 1, 1), Amount: -1}); err == nil {
		t.Error("负金额应报错")
	}
}
