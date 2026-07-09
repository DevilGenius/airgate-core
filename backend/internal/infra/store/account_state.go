package store

const accountManualClosedReason = "手动关闭"

func isClosedAccountErrorMsg(errorMsg string) bool {
	return errorMsg == "" || errorMsg == accountManualClosedReason
}
