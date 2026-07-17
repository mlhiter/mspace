//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsAgentEngineProcessUsesKillOnJobClose(t *testing.T) {
	info := jobObjectExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	if info.BasicLimitInformation.LimitFlags != 0x00002000 {
		t.Fatalf("unexpected Job Object kill flag: %#x", info.BasicLimitInformation.LimitFlags)
	}
	if unsafe.Sizeof(info) == 0 {
		t.Fatal("Windows Job Object limit structure is empty")
	}
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	configureAgentEngineProcess(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("Agent process is not configured as a Windows process group: %+v", cmd.SysProcAttr)
	}
}
