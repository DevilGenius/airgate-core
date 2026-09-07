//go:build windows

package billing

// The event itself is flushed before its NTFS hard-link publication. Windows
// does not expose directory fsync through os.File; production Docker uses the
// Unix implementation, which also persists the directory entry before ACK.
func syncJournalDirectory(string) error { return nil }
