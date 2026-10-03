package main

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"unicode/utf16"
)

/* Windows limits one file or folder name to 255 characters, whatever the total
   path length (long total paths work: the file APIs warpseed uses accept them).
   Servers cap names at 255 bytes too, so a real name nearly always fits, and
   the in-progress file of a name that is close to the limit gets its own
   stand-in (sftpfast.PartPath). Only a name that cannot exist on Windows at
   all is shortened here: the start of the name, a short hash so two long
   names never collide, and the extension. */

const maxLocalNameUnits = 255

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// fitName returns name unchanged if Windows can hold it, or a shortened name
// that keeps the extension, and whether it had to shorten it.
func fitName(name string) (string, bool) {
	if utf16Len(name) <= maxLocalNameUnits {
		return name, false
	}
	ext := filepath.Ext(name)
	if utf16Len(ext) > 20 {
		ext = ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	tail := fmt.Sprintf("~%08x%s", h.Sum32(), ext)
	budget := maxLocalNameUnits - utf16Len(tail)
	var head []rune
	for _, r := range name[:len(name)-len(ext)] {
		n := utf16Len(string(r))
		if budget < n {
			break
		}
		budget -= n
		head = append(head, r)
	}
	return string(head) + tail, true
}
