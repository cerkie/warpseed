//go:build !windows

package localfs

import "os"

// sendToTrash has no portable Recycle Bin here, so it deletes outright.
func sendToTrash(path string) error { return os.RemoveAll(path) }
