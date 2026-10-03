package localfs

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSHFileOperation = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// shFileOp mirrors SHFILEOPSTRUCTW; Go's natural alignment matches the
// native layout on both 32- and 64-bit Windows.
type shFileOp struct {
	Hwnd                 uintptr
	Func                 uint32
	From                 *uint16
	To                   *uint16
	Flags                uint16
	AnyOperationsAborted int32
	NameMappings         uintptr
	ProgressTitle        *uint16
}

const (
	foDelete          = 0x3
	fofSilent         = 0x4
	fofNoConfirmation = 0x10
	fofAllowUndo      = 0x40
	fofNoErrorUI      = 0x400
)

// sendToTrash moves a file or folder to the Recycle Bin. A volume with no bin
// (a network share, say) is deleted outright by the shell, as Explorer does.
func sendToTrash(path string) error {
	if len(path) >= 260 {
		return ErrBinTooLong
	}
	from, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0) // the shell wants a double-null-terminated list
	op := shFileOp{
		Func:  foDelete,
		From:  &from[0],
		Flags: fofAllowUndo | fofNoConfirmation | fofNoErrorUI | fofSilent,
	}
	if r, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op))); r != 0 {
		if treeHasLongPath(path) {
			return ErrBinTooLong
		}
		return fmt.Errorf("recycle bin refused it (error 0x%x)", r)
	}
	if op.AnyOperationsAborted != 0 {
		return fmt.Errorf("recycle bin operation was cancelled")
	}
	return nil
}
