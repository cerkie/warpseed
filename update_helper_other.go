//go:build !windows

package main

import "errors"

func startUpdateHelper(kind, src, target string) error {
	return errors.New("installing updates is only available on Windows")
}

func applyUpdate([]string) {}
