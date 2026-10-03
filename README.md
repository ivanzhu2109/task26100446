# indtax

可追溯的间接税核算库（纯进程内，无外部依赖）。税则及其不可变历史保存在 `Ledger` 中，
支持登记税则变更与批量历史核算，可精确复现"申报当时掌握的税则"下的历史结论。

## 核心概念

- **知悉序号**：每次 `Register` 获得全局严格递增序号（从 1 开始），已登记内容不可修改。
- **核算**：每笔交易指定交易日期与知悉序号上界（当时掌握的信息）。
  同一适用范围内，取序号不超过上界且生效区间（含起日、不含止日，止日可为空）
  覆盖交易日期的变更中序号最大者。
- **范围回退**：先看商品专属范围，无适用变更或结果为撤销时再看全部商品范围；
  撤销只让本范围失去适用资格，不会复活更早的税率；两范围均无果时结论为无规则。
- **税率**：保留四位小数的整数，`130000` 表示 13.0000%。税额逐笔四舍五入到整数
  最小货币单位，恰好半单位向上取整，使用大整数运算，无浮点误差。

## 示例

```go
l := indtax.NewLedger()
seq1, _ := l.Register(indtax.Change{
    Jurisdiction: "CN", TaxSystem: "VAT",
    Kind: indtax.KindTaxable, Rate: 130000, // 13.0000%
    EffectiveFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
})
res, _ := l.Assess(indtax.Transaction{
    Jurisdiction: "CN", TaxSystem: "VAT", Category: "book",
    Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
    MaxSeq: seq1, Amount: 10000,
})
// res = {Status: StatusTaxable, Rate: 130000, Tax: 1300}
```

## 测试

```sh
go test ./...
```
