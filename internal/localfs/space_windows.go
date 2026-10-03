//go:build windows

package localfs

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// DiskSpace reports the free/total bytes of the volume holding path. Only the
// volume root is asked about, so a folder path past 260 characters still works.
func DiskSpace(path string) (Space, error) {
	root := path
	if abs, err := filepath.Abs(path); err == nil {
		if vol := filepath.VolumeName(abs); vol != "" {
			root = vol + `\`
		}
	}
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return Space{}, err
	}
	var avail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &totalFree); err != nil {
		return Space{}, err
	}
	return Space{Free: int64(avail), Total: int64(total)}, nil
}
