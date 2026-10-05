package accountweight

const (
	Default = 100
	Min     = 0
	Max     = 1000000
)

func Valid(value int) bool { return value >= Min && value <= Max }
