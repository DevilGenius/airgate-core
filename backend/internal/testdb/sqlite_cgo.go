//go:build cgo

package testdb

import _ "github.com/mattn/go-sqlite3"

const sqliteDriverName = "sqlite3"
