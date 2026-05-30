//go:build !cgo

package testdb

import _ "modernc.org/sqlite"

const sqliteDriverName = "sqlite"
