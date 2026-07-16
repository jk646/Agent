//go:build linux

package session

import (
	"os"
	"syscall"
	"unsafe"
)

func signalForeground(ptmx *os.File, fallbackPID int, signal syscall.Signal) error {
	var processGroup int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(), uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&processGroup)))
	if errno == 0 && processGroup > 0 {
		return syscall.Kill(-int(processGroup), signal)
	}
	return syscall.Kill(-fallbackPID, signal)
}

func disableEcho(ptmx *os.File) error {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)))
	if errno != 0 {
		return errno
	}
	termios.Lflag &^= syscall.ECHO
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&termios)))
	if errno != 0 {
		return errno
	}
	return nil
}
