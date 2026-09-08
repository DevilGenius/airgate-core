//go:build cgo

package billing

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

func cgoSQLiteConstraintError(err error) bool {
	var sqlite sqlite3.Error
	return errors.As(err, &sqlite) && sqlite.Code == sqlite3.ErrConstraint
}
