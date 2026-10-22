package businessstate

import "context"

// BudgetTotals 是全部 epoch 的预算合计，不生成随 epoch 增长的指标标签。
type BudgetTotals struct {
	OpenRequirements uint64
	ParkedMessages   uint64
	RetainedGroups   uint64
	RetainedBytes    uint64
}

// 预算表为空时保留 NULL 扫描错误，避免把未初始化的预算当作已排空。
func (repository *Repository) BudgetTotals(ctx context.Context) (BudgetTotals, error) {
	var value BudgetTotals
	err := repository.database.QueryRowContext(ctx, `SELECT SUM(open_requirements), SUM(parked_messages), SUM(retained_groups), SUM(retained_bytes) FROM business_async_budget`).Scan(
		&value.OpenRequirements, &value.ParkedMessages, &value.RetainedGroups, &value.RetainedBytes)
	return value, err
}
