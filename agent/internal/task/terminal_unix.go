//go:build !windows

package task

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// terminalIO 基于 PTY 的终端 I/O：读写、窗口尺寸调整、关闭。
// PTY 让交互式程序（vim/top 等）获得完整 TTY 语义与颜色控制序列。
type terminalIO struct {
	tty *os.File
}

func startTerminal(cmd *exec.Cmd, cols, rows int) (*terminalIO, error) {
	// 通过 PTY 启动子进程，stdout/stderr 合并到同一终端
	t, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	if cols > 0 && rows > 0 {
		_ = pty.Setsize(t, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
	return &terminalIO{tty: t}, nil
}

func (t *terminalIO) Read(p []byte) (int, error)  { return t.tty.Read(p) }
func (t *terminalIO) Write(p []byte) (int, error) { return t.tty.Write(p) }

func (t *terminalIO) Resize(cols, rows int) error {
	if t.tty == nil {
		return nil
	}
	return pty.Setsize(t.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (t *terminalIO) Close() error {
	if t.tty == nil {
		return nil
	}
	return t.tty.Close()
}
