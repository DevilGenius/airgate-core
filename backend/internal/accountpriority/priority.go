package accountpriority

const (
	Min = -99999
	Max = 99999
)

// Clamp 将账号优先级限制在系统支持的范围内。
func Clamp(value int) int {
	if value < Min {
		return Min
	}
	if value > Max {
		return Max
	}
	return value
}

// AddOffset 在不越界的前提下为账号优先级增加偏移量。
func AddOffset(current, offset int) (int, bool) {
	if current < Min || current > Max {
		return 0, false
	}
	if offset > 0 && offset > Max-current {
		return 0, false
	}
	if offset < 0 && offset < Min-current {
		return 0, false
	}
	return current + offset, true
}
