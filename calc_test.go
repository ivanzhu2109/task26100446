package taxcalc

import "testing"

func d(y, m, day int) Date { return Date{Year: y, Month: m, Day: day} }

func datePtr(dt Date) *Date { return &dt }

func mustRate(t *testing.T, s string) Rate {
	t.Helper()
	r, err := RateFromString(s)
	if err != nil {
		t.Fatalf("RateFromString(%q): %v", s, err)
	}
	return r
}

// 追溯更正与原区间部分重叠：更正只覆盖自身生效区间，
// 且只有更正已被知悉（知悉序号上界足够）且日期被覆盖时结果才改变。
func TestRetroactiveCorrectionPartialOverlap(t *testing.T) {
	reg := NewRegistry()
	// 原规定：全部商品 2024-01-01 起 13%，无限延续。
	seq1 := reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "13.0000"),
		From: d(2024, 1, 1),
	})
	// 追溯更正：仅 2024-03-01（含）至 2024-06-01（不含）降为 9%。
	seq2 := reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "9.0000"),
		From: d(2024, 3, 1), To: datePtr(d(2024, 6, 1)),
	})
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("知悉序号应严格递增: got %d, %d", seq1, seq2)
	}

	cases := []struct {
		name    string
		date    Date
		maxSeq  uint64
		wantTax int64
	}{
		// 更正前日期：无论是否知悉更正，仍按 13%。
		{"更正区间前/未知悉更正", d(2024, 2, 28), seq1, 13},
		{"更正区间前/已知悉更正", d(2024, 2, 28), seq2, 13},
		// 恰落在更正起日（含）：已知悉时变为 9%，未知悉时仍 13%。
		{"更正起日/未知悉更正", d(2024, 3, 1), seq1, 13},
		{"更正起日/已知悉更正", d(2024, 3, 1), seq2, 9},
		// 区间内。
		{"区间内/已知悉更正", d(2024, 5, 31), seq2, 9},
		// 恰落在更正止日（不含）：恢复 13%。
		{"更正止日/已知悉更正", d(2024, 6, 1), seq2, 13},
		// 区间后。
		{"更正区间后/已知悉更正", d(2024, 7, 1), seq2, 13},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reg.Compute(Tx{
				Jurisdiction: "CN", TaxSystem: "VAT",
				Category: "goods", Date: tc.date, Amount: 100,
			}, tc.maxSeq)
			if got.Conclusion != ConclusionTaxable {
				t.Fatalf("结论 = %v, 期望应税", got.Conclusion)
			}
			if got.Tax != tc.wantTax {
				t.Fatalf("税额 = %d, 期望 %d", got.Tax, tc.wantTax)
			}
		})
	}
}

// 专属免税遮蔽通用税率 → 专属撤销后回退通用规则 →
// 通用规则也撤销后变为无规则；旧专属税率不得复活。
func TestScopeFallbackChain(t *testing.T) {
	reg := NewRegistry()
	// seq1: 全部商品 13%。
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "13.0000"),
		From: d(2024, 1, 1),
	})
	// seq2: 图书专属应税 9%（之后被撤销，不得复活）。
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "books",
		Kind: KindTaxable, Rate: mustRate(t, "9.0000"),
		From: d(2024, 1, 1),
	})
	// seq3: 图书专属免税（登记在后，遮蔽通用 13% 与专属 9%）。
	seqExempt := reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "books",
		Kind: KindExempt,
		From: d(2024, 1, 1),
	})
	// seq4: 撤销图书专属规定 → 回退全部商品 13%。
	seqRevokeSpecific := reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "books",
		Kind: KindRevoke,
		From: d(2024, 1, 1),
	})
	// seq5: 撤销全部商品规定 → 无规则。
	seqRevokeGeneral := reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindRevoke,
		From: d(2024, 1, 1),
	})

	tx := Tx{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Category: "books", Date: d(2024, 2, 1), Amount: 100,
	}

	// 阶段一：专属免税遮蔽通用税率。
	got := reg.Compute(tx, seqExempt)
	if got.Conclusion != ConclusionExempt || got.Tax != 0 {
		t.Fatalf("专属免税阶段: got %+v, 期望免税且税额为 0", got)
	}

	// 阶段二：专属撤销 → 回退通用 13%，不得复活专属 9%。
	got = reg.Compute(tx, seqRevokeSpecific)
	if got.Conclusion != ConclusionTaxable || got.Tax != 13 {
		t.Fatalf("专属撤销阶段: got %+v, 期望应税 13", got)
	}

	// 阶段三：通用也撤销 → 无规则，不能冒充零税额。
	got = reg.Compute(tx, seqRevokeGeneral)
	if got.Conclusion != ConclusionNoRule {
		t.Fatalf("通用撤销阶段: got %+v, 期望无规则", got)
	}

	// 其他商品不受专属撤销影响，仍按通用 13%（通用撤销前）。
	other := tx
	other.Category = "food"
	got = reg.Compute(other, seqRevokeSpecific)
	if got.Conclusion != ConclusionTaxable || got.Tax != 13 {
		t.Fatalf("其他商品: got %+v, 期望应税 13", got)
	}
}

// 新增追溯更正后，按旧知悉序号核算的结果必须完全不变。
func TestOldKnowledgeSeqStable(t *testing.T) {
	reg := NewRegistry()
	seq1 := reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "13.0000"),
		From: d(2024, 1, 1),
	})
	txs := []Tx{
		{Jurisdiction: "CN", TaxSystem: "VAT", Category: "a", Date: d(2024, 3, 1), Amount: 100},
		{Jurisdiction: "CN", TaxSystem: "VAT", Category: "b", Date: d(2024, 4, 1), Amount: 777},
	}
	before := reg.ComputeBatch(txs, seq1)

	// 登记追溯更正与撤销。
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "9.0000"),
		From: d(2024, 1, 1),
	})
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindRevoke, From: d(2024, 1, 1),
	})

	after := reg.ComputeBatch(txs, seq1)
	if before != nil && len(before) != len(after) {
		t.Fatalf("批量结果长度变化")
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("第 %d 笔历史核算被污染: before=%+v after=%+v", i, before[i], after[i])
		}
	}
}

// 税额四舍五入：恰好半单位向上取整，无浮点误差。
func TestRoundingHalfUp(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "50.0000"),
		From: d(2024, 1, 1),
	})
	cases := []struct {
		amount int64
		want   int64
	}{
		{0, 0},  // 0 * 50% = 0
		{1, 1},  // 0.5 → 向上取整为 1
		{2, 1},  // 1.0 → 1
		{3, 2},  // 1.5 → 2
		{5, 3},  // 2.5 → 3
		{10, 5}, // 5.0 → 5
	}
	for _, tc := range cases {
		got := reg.Compute(Tx{
			Jurisdiction: "CN", TaxSystem: "VAT",
			Category: "x", Date: d(2024, 1, 1), Amount: tc.amount,
		}, 1)
		if got.Conclusion != ConclusionTaxable || got.Tax != tc.want {
			t.Fatalf("amount=%d: got %+v, 期望税额 %d", tc.amount, got, tc.want)
		}
	}
}

// 零税率应税不触发回退，且与无规则区分；免税税额为 0。
func TestZeroRateAndExempt(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "13.0000"),
		From: d(2024, 1, 1),
	})
	// 专属零税率：应税 0%，不回退到通用 13%。
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT", Category: "zero",
		Kind: KindTaxable, Rate: 0,
		From: d(2024, 1, 1),
	})
	got := reg.Compute(Tx{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Category: "zero", Date: d(2024, 1, 1), Amount: 100,
	}, 2)
	if got.Conclusion != ConclusionTaxable || got.Tax != 0 {
		t.Fatalf("零税率: got %+v, 期望应税且税额为 0", got)
	}

	// 无规则与零税额严格区分。
	got = reg.Compute(Tx{
		Jurisdiction: "CN", TaxSystem: "OTHER",
		Category: "zero", Date: d(2024, 1, 1), Amount: 100,
	}, 2)
	if got.Conclusion != ConclusionNoRule {
		t.Fatalf("未知税制: got %+v, 期望无规则", got)
	}
}

// 生效区间边界：含起日、不含止日；止日为空表示无限延续。
func TestEffectiveRangeBoundaries(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "10.0000"),
		From: d(2024, 1, 1), To: datePtr(d(2024, 2, 1)),
	})
	tx := Tx{Jurisdiction: "CN", TaxSystem: "VAT", Category: "x", Amount: 100}

	got := reg.Compute(Tx{Jurisdiction: tx.Jurisdiction, TaxSystem: tx.TaxSystem, Category: tx.Category, Date: d(2024, 1, 1), Amount: 100}, 1)
	if got.Conclusion != ConclusionTaxable || got.Tax != 10 {
		t.Fatalf("起日应覆盖: got %+v", got)
	}
	got = reg.Compute(Tx{Jurisdiction: tx.Jurisdiction, TaxSystem: tx.TaxSystem, Category: tx.Category, Date: d(2024, 2, 1), Amount: 100}, 1)
	if got.Conclusion != ConclusionNoRule {
		t.Fatalf("止日不应覆盖: got %+v", got)
	}

	// 无限延续。
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "5.0000"),
		From: d(2024, 2, 1),
	})
	got = reg.Compute(Tx{Jurisdiction: tx.Jurisdiction, TaxSystem: tx.TaxSystem, Category: tx.Category, Date: d(2030, 12, 31), Amount: 100}, 2)
	if got.Conclusion != ConclusionTaxable || got.Tax != 5 {
		t.Fatalf("无限延续: got %+v", got)
	}
}

// 不同辖区与税制之间不能回退。
func TestNoCrossJurisdictionFallback(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "13.0000"),
		From: d(2024, 1, 1),
	})
	got := reg.Compute(Tx{
		Jurisdiction: "US", TaxSystem: "VAT",
		Category: "x", Date: d(2024, 1, 1), Amount: 100,
	}, 1)
	if got.Conclusion != ConclusionNoRule {
		t.Fatalf("跨辖区: got %+v, 期望无规则", got)
	}
	got = reg.Compute(Tx{
		Jurisdiction: "CN", TaxSystem: "GST",
		Category: "x", Date: d(2024, 1, 1), Amount: 100,
	}, 1)
	if got.Conclusion != ConclusionNoRule {
		t.Fatalf("跨税制: got %+v, 期望无规则", got)
	}
}

// 登记内容不可变：调用方修改原 Change 不影响已登记内容。
func TestRegisteredChangeImmutable(t *testing.T) {
	reg := NewRegistry()
	to := d(2024, 2, 1)
	c := Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "13.0000"),
		From: d(2024, 1, 1), To: &to,
	}
	reg.Register(c)
	// 修改调用方持有的值。
	c.Rate = mustRate(t, "99.0000")
	to = d(2020, 1, 1)

	got := reg.Compute(Tx{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Category: "x", Date: d(2024, 1, 15), Amount: 100,
	}, 1)
	if got.Tax != 13 {
		t.Fatalf("已登记内容被修改: got %+v", got)
	}
	got = reg.Compute(Tx{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Category: "x", Date: d(2024, 3, 1), Amount: 100,
	}, 1)
	if got.Conclusion != ConclusionNoRule {
		t.Fatalf("止日被污染: got %+v", got)
	}
}

// 区间嵌套时取知悉序号最大且覆盖交易日期的变更。
func TestNestedIntervals(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "13.0000"),
		From: d(2024, 1, 1), To: datePtr(d(2024, 12, 31)),
	})
	// 嵌套小区间，先登记先生效无关。
	reg.Register(Change{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Kind: KindTaxable, Rate: mustRate(t, "6.0000"),
		From: d(2024, 3, 1), To: datePtr(d(2024, 4, 1)),
	})
	got := reg.Compute(Tx{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Category: "x", Date: d(2024, 3, 15), Amount: 100,
	}, 2)
	if got.Tax != 6 {
		t.Fatalf("嵌套区间内: got %+v, 期望 6", got)
	}
	got = reg.Compute(Tx{
		Jurisdiction: "CN", TaxSystem: "VAT",
		Category: "x", Date: d(2024, 5, 15), Amount: 100,
	}, 2)
	if got.Tax != 13 {
		t.Fatalf("嵌套区间外: got %+v, 期望 13", got)
	}
}
