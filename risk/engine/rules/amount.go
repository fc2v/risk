package rules

func size(amount int64) int64 {
	if amount < 0 {
		return -amount
	}
	return amount
}
