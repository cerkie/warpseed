//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

/* The last step of an update. A running exe cannot be replaced, so warpseed
   starts a copy of itself from a temp folder with --apply-update and quits. The
   copy waits for the old process to end, installs, and starts warpseed again.
   It always starts warpseed again, the new version or the old one, so a failed
   install never leaves the user with no app. */

func startUpdateHelper(kind, src, target string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "warpseed-updater")
	if err != nil {
		return err
	}
	helper := filepath.Join(dir, "warpseed-updater.exe")
	if err := copyFile(self, helper); err != nil {
		return err
	}
	cmd := exec.Command(helper, "--apply-update", strconv.Itoa(os.Getpid()), kind, src, target)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	return cmd.Start()
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// applyUpdate is the helper's whole life: args are pid, kind, source, target.
func applyUpdate(args []string) {
	if len(args) < 4 {
		return
	}
	pid, _ := strconv.Atoi(args[0])
	err := runApply(pid, args[1], args[2], args[3])
	msg := "ok"
	if err != nil {
		msg = err.Error()
	}
	_ = os.WriteFile(updateResultPath(), []byte(msg), 0o600)
	_ = exec.Command(args[3]).Start()
}

func runApply(pid int, kind, src, target string) error {
	waitForExit(pid, 2*time.Minute)
	switch kind {
	case "installer":
		// `start /wait` asks Windows for the installer's elevation prompt and
		// waits for it. /D= must be last and unquoted, which is why the command
		// line is written out by hand.
		cmd := exec.Command("cmd.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
			CmdLine:    fmt.Sprintf(`cmd.exe /C start /wait "" "%s" /S /D=%s`, src, filepath.Dir(target)),
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("the installer did not finish (%v)", err)
		}
		return nil
	case "portable":
		return swapExecutable(target, src)
	}
	return fmt.Errorf("unknown update kind %q", kind)
}

// waitForExit returns once the process is gone, or after the timeout.
func waitForExit(pid int, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return // already gone
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
}
