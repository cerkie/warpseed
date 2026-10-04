package main

import (
	"fmt"

	"warpseed/internal/localfs"
)

// byteText writes a byte count the way the file panes do: 1.4 GiB, 612 MiB.
func byteText(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// spaceShortfall says whether need bytes will not fit in free, and by how much.
// A zero free value means the volume could not be read, which is not a warning.
func spaceShortfall(need, free int64) (short bool, by int64) {
	if need <= 0 || free <= 0 || need <= free {
		return false, 0
	}
	return true, need - free
}

// warnLowSpace tells the user when a batch of downloads will not fit where it
// is going. It only warns: the files are queued either way, because free space
// moves (something may finish, or be deleted) and a hard refusal would be
// wrong more often than a heads-up.
func (a *App) warnLowSpace(localDir string, need int64) {
	if a.store == nil || a.store.Setting("transfers.space_warning", "1") != "1" {
		return
	}
	sp, err := localfs.DiskSpace(localDir)
	if err != nil {
		return
	}
	if short, by := spaceShortfall(need, sp.Free); short {
		a.sink.Emit("app:error", fmt.Sprintf(
			"Not enough free space for these downloads: they need %s and %s is free (%s short). They are queued, but will fail once the disk is full.",
			byteText(need), byteText(sp.Free), byteText(by)))
	}
}
