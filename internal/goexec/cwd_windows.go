//go:build windows

package goexec

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"path/filepath"
	"unsafe"
)

var (
	modNtdll   = windows.NewLazySystemDLL("ntdll.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procNtQueryInformationProcess = modNtdll.NewProc("NtQueryInformationProcess")
	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
)

const processBasicInformationClass = 0

type processBasicInformation struct {
	ExitStatus                   uintptr
	PebBaseAddress               uintptr
	AffinityMask                 uintptr
	BasePriority                 uintptr
	UniqueProcessID              uintptr
	InheritedFromUniqueProcessID uintptr
}

type peb struct {
	Reserved1         [2]byte
	BeingDebugged     byte
	Reserved2         byte
	Reserved3         [2]uintptr
	Ldr               uintptr
	ProcessParameters uintptr
	Reserved4         [3]uintptr
	Reserved5         uintptr
	Reserved6         [5]uintptr
}

type rtlUserProcessParameters struct {
	Reserved1     [16]byte
	Reserved2     [4]uintptr
	ImagePathName struct {
		Length    uint16
		MaxLength uint16
		Buffer    uintptr
	}
	CommandLine struct {
		Length    uint16
		MaxLength uint16
		Buffer    uintptr
	}
	CurrentDirectoryPath struct {
		Length    uint16
		MaxLength uint16
		Buffer    uintptr
	}
	DllPath struct {
		Length    uint16
		MaxLength uint16
		Buffer    uintptr
	}
	CurrentDirectory string
}

func CurrentWorkingDirectoryForPid(pid int) (string, error) {
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ,
		false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(handle)
		dir, err := readProcessCwd(handle, pid)
		if err == nil {
			return dir, nil
		}
		// PEB read failed (e.g. access denied), fallback to image path.
	}

	// Try fallback: get executable path directory as CWD approximation.
	dir, err2 := queryProcessImagePath(pid)
	if err2 != nil {
		return "", errors.Errorf("process %d is not accessible or no longer exists; "+
			"set JUPYTER_DATA_DIR environment variable or run Jupyter from a directory you have access to",
			pid)
	}
	return dir, nil
}

func readProcessCwd(handle windows.Handle, pid int) (string, error) {
	var pbi processBasicInformation
	var returnLength uint32
	procNtQueryInformationProcess.Call(
		uintptr(handle),
		processBasicInformationClass,
		uintptr(unsafe.Pointer(&pbi)),
		uintptr(unsafe.Sizeof(pbi)),
		uintptr(unsafe.Pointer(&returnLength)),
	)
	if pbi.PebBaseAddress == 0 {
		return "", errors.Errorf("failed to query process basic information for pid %d", pid)
	}

	var processPeb peb
	if err := readProcessMemory(handle, pbi.PebBaseAddress, (*byte)(unsafe.Pointer(&processPeb)), unsafe.Sizeof(processPeb)); err != nil {
		return "", err
	}

	if processPeb.ProcessParameters == 0 {
		return "", errors.Errorf("process parameters not found for pid %d", pid)
	}

	var params rtlUserProcessParameters
	if err := readProcessMemory(handle, processPeb.ProcessParameters, (*byte)(unsafe.Pointer(&params)), unsafe.Sizeof(params)); err != nil {
		return "", err
	}

	if params.CurrentDirectoryPath.Buffer == 0 || params.CurrentDirectoryPath.Length == 0 {
		return "", errors.Errorf("current directory not available for pid %d", pid)
	}

	buf := make([]uint16, params.CurrentDirectoryPath.Length/2)
	if err := readProcessMemory(handle, params.CurrentDirectoryPath.Buffer, (*byte)(unsafe.Pointer(&buf[0])), uintptr(params.CurrentDirectoryPath.Length)); err != nil {
		return "", err
	}

	return windows.UTF16ToString(buf), nil
}

func queryProcessImagePath(pid int) (string, error) {
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(pid))
	if err != nil {
		return "", errors.Wrapf(err, "failed to open process %d", pid)
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, windows.MAX_PATH+1)
	var size uint32 = uint32(len(buf))
	err = procQueryFullProcessImageNameW.Find()
	if err != nil {
		return "", errors.Wrapf(err, "QueryFullProcessImageNameW not available")
	}
	r1, _, _ := procQueryFullProcessImageNameW.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return "", errors.Errorf("QueryFullProcessImageNameW failed for pid %d", pid)
	}

	exePath := windows.UTF16ToString(buf[:size])
	return filepath.Dir(exePath), nil
}

func readProcessMemory(handle windows.Handle, baseAddress uintptr, buffer *byte, size uintptr) error {
	var nread uintptr
	err := windows.ReadProcessMemory(handle, baseAddress, buffer, size, &nread)
	if err != nil {
		return err
	}
	if nread != size {
		return errors.Errorf("ReadProcessMemory read %d bytes, expected %d", nread, size)
	}
	return nil
}
