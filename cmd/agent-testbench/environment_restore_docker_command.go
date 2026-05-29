package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func runRestoreGitCommand(ctx context.Context, args ...string) (string, string) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, err.Error()
	}
	return output, ""
}

func runRestoreCommand(ctx context.Context, workdir string, command []string) (string, string) {
	if len(command) == 0 {
		return "", "empty restore command"
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	configureRestoreCommandCancellation(cmd)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output != "" {
			return output, err.Error() + ": " + output
		}
		return output, err.Error()
	}
	return output, ""
}

func runRestoreCommandWithInput(ctx context.Context, workdir string, command []string, input string) (string, string) {
	if len(command) == 0 {
		return "", "empty restore command"
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	configureRestoreCommandCancellation(cmd)
	cmd.Dir = workdir
	cmd.Stdin = bytes.NewBufferString(input)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output != "" {
			return output, err.Error() + ": " + output
		}
		return output, err.Error()
	}
	return output, ""
}

func configureRestoreCommandCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err == syscall.ESRCH {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
}
