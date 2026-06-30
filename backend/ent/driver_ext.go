package ent

import "entgo.io/ent/dialect"

// Driver exposes the underlying ent driver for package-local infrastructure
// that needs to execute dialect-aware raw SQL while keeping ent transactions.
func (tx *Tx) Driver() dialect.Driver {
	return tx.driver
}

// Driver exposes the underlying ent driver for package-local infrastructure
// that needs to execute dialect-aware raw SQL.
func (c *Client) Driver() dialect.Driver {
	return c.driver
}
