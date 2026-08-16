//go:build windows

package task

import (
	"io"
	"os/exec"
)

type terminalIO struct {
	in  io.WriteCloser
	out io.ReadCloser
}

func startTerminal(cmd *exec.Cmd, _, _ int) (*terminalIO, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	return &terminalIO{in: in, out: out}, nil
}
func (t *terminalIO) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *terminalIO) Write(p []byte) (int, error) { return t.in.Write(p) }
func (t *terminalIO) Resize(_, _ int) error       { return nil }
func (t *terminalIO) Close() error                { _ = t.in.Close(); return t.out.Close() }
