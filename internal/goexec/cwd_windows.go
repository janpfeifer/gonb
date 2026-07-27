//go:build windows && !386

package goexec

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"path/filepath"
	"unsafe"
)

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	_             uint32 // Padding on x64
	Buffer        uintptr
}

type processBasicInformation struct {
	ExitStatus                   uintptr
	PebBaseAddress               uintptr
	AffinityMask                 uintptr
	BasePriority                 uintptr
	UniqueProcessID              uintptr
	InheritedFromUniqueProcessID uintptr
}

// Partial RTL_USER_PROCESS_PARAMETERS layout for 64-bit Windows.
type rtlUserProcessParameters struct {
	_                    [32]byte
	_                    [3]windows.Handle
	CurrentDirectoryPath unicodeString // Offset 0x38 on x64
}

func CurrentWorkingDirectoryForPid(pid int) (string, error) {
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ,
		false,
		uint32(pid),
	)
	if err != nil {
		return queryProcessImagePath(pid)
	}
	defer windows.CloseHandle(handle)

	dir, err := readProcessCwd(handle, pid)
	if err != nil {
		return queryProcessImagePath(pid)
	}
	return dir, nil
}

func readProcessCwd(handle windows.Handle, pid int) (string, error) {
	var pbi processBasicInformation
	var retLen uint32
	status := windows.NtQueryInformationProcess(
		handle,
		0,
		unsafe.Pointer(&pbi),
		uint32(unsafe.Sizeof(pbi)),
		&retLen,
	)
	if status != nil {
		return "", errors.Errorf("NtQueryInformationProcess failed: %v", status)
	}
	if pbi.PebBaseAddress == 0 {
		return "", errors.Errorf("PEB base address is null for pid %d", pid)
	}

	var procParamsPtr uintptr
	var bytesRead uintptr
	err := windows.ReadProcessMemory(
		handle, pbi.PebBaseAddress+0x20,
		(*byte)(unsafe.Pointer(&procParamsPtr)),
		uintptr(unsafe.Sizeof(procParamsPtr)),
		&bytesRead,
	)
	if err != nil {
		return "", err
	}

	var params rtlUserProcessParameters
	err = windows.ReadProcessMemory(
		handle, procParamsPtr,
		(*byte)(unsafe.Pointer(&params)),
		uintptr(unsafe.Sizeof(params)),
		&bytesRead,
	)
	if err != nil {
		return "", err
	}

	if params.CurrentDirectoryPath.Buffer == 0 || params.CurrentDirectoryPath.Length == 0 {
		return "", errors.Errorf("current directory not available for pid %d", pid)
	}

	buf := make([]uint16, params.CurrentDirectoryPath.Length/2)
	err = windows.ReadProcessMemory(
		handle,
		params.CurrentDirectoryPath.Buffer,
		(*byte)(unsafe.Pointer(&buf[0])),
		uintptr(params.CurrentDirectoryPath.Length),
		&bytesRead,
	)
	if err != nil {
		return "", err
	}

	return windows.UTF16ToString(buf), nil
}

func queryProcessImagePath(pid int) (string, error) {
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(pid))
	if err != nil {
		return "", errors.Errorf("process %d is not accessible or no longer exists; "+
			"set JUPYTER_DATA_DIR environment variable or run Jupyter from a directory you have access to",
			pid)
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, windows.MAX_PATH+1)
	var size uint32 = uint32(len(buf))
	err = windows.QueryFullProcessImageName(handle, 0, &buf[0], &size)
	if err != nil {
		return "", err
	}

	return filepath.Dir(windows.UTF16ToString(buf[:size])), nil
}
