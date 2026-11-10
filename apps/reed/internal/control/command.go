package control

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// Command is the process boundary for gh, swe and an explicitly configured Agent host.
type Command func(context.Context, string, []string, string, []byte) ([]byte, int, error)

func Execute(ctx context.Context, program string, args []string, dir string, input []byte) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	configureProcess(cmd)
	cmd.Dir = dir
	// Also bound inherited pipes after cancelling the process tree.
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(input)
	var out, stderr limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return out.Bytes(), 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.Bytes(), exit.ExitCode(), errors.New(stderr.String() + ": " + err.Error())
	}
	return out.Bytes(), -1, err
}

// Bound external output so a child cannot exhaust the management process memory.
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8<<20 {
		return 0, errors.New("command output exceeds 8 MiB")
	}
	return b.Buffer.Write(p)
}
