//go:build !cgo

package billing

func cgoSQLiteConstraintError(error) bool { return false }
