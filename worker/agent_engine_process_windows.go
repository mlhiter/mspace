//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"
)

const (
	jobObjectInfoClassExtendedLimit = 9
	jobObjectLimitKillOnJobClose    = 0x00002000
	processSetQuota                 = 0x0100
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
)

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type windowsAgentEngineJob struct {
	handle syscall.Handle
	once   sync.Once
	err    error
}

func (j *windowsAgentEngineJob) Close() error {
	if j == nil {
		return nil
	}
	j.once.Do(func() {
		j.err = syscall.CloseHandle(j.handle)
	})
	return j.err
}

func configureAgentEngineProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func attachAgentEngineProcessTree(cmd *exec.Cmd) (io.Closer, error) {
	if cmd == nil || cmd.Process == nil {
		return nil, fmt.Errorf("attach Windows Job Object: process is not started")
	}
	jobHandle, _, callErr := procCreateJobObjectW.Call(0, 0)
	if jobHandle == 0 {
		return nil, fmt.Errorf("create Windows Job Object: %w", normalizeWindowsCallError(callErr))
	}
	job := &windowsAgentEngineJob{handle: syscall.Handle(jobHandle)}
	info := jobObjectExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	result, _, callErr := procSetInformationJobObject.Call(
		jobHandle,
		jobObjectInfoClassExtendedLimit,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if result == 0 {
		_ = job.Close()
		return nil, fmt.Errorf("configure Windows Job Object: %w", normalizeWindowsCallError(callErr))
	}
	processHandle, err := syscall.OpenProcess(processSetQuota|syscall.PROCESS_TERMINATE|syscall.PROCESS_QUERY_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = job.Close()
		return nil, fmt.Errorf("open Agent process for Windows Job Object: %w", err)
	}
	defer syscall.CloseHandle(processHandle)
	result, _, callErr = procAssignProcessToJobObject.Call(jobHandle, uintptr(processHandle))
	if result == 0 {
		_ = job.Close()
		return nil, fmt.Errorf("assign Agent process to Windows Job Object: %w", normalizeWindowsCallError(callErr))
	}
	return job, nil
}

func normalizeWindowsCallError(err error) error {
	if err == nil || err == syscall.Errno(0) {
		return syscall.EINVAL
	}
	return err
}

func interruptAgentEngineProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}

func killAgentEngineProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		if err := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run(); err != nil {
			_ = cmd.Process.Kill()
		}
	}
}
