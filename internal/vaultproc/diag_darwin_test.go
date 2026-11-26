//go:build darwin

package vaultproc

// Temporary macOS diagnosis, to be removed: logs why the kit's peer check may refuse a stand-in vault
// process. It runs a second stand-in of its own, so the test it is called from is not disturbed.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// macDiagOwn is this program as it was when the test binary started.
var macDiagOwnPath, macDiagOwnInfo = func() (string, os.FileInfo) {
	path, err := os.Executable()
	if err != nil {
		return "", nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, nil
	}
	info, _ := os.Stat(resolved)
	return resolved, info
}()

func macDiagLog(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Logf("macdiag: "+format, args...)
}

// macDiagChain tells an error with its type chain and the typed facts of the kit.
func macDiagChain(err error) string {
	if err == nil {
		return "nil"
	}
	var types []string
	var walk func(error)
	walk = func(e error) {
		for e != nil {
			types = append(types, fmt.Sprintf("%T", e))
			if multi, ok := e.(interface{ Unwrap() []error }); ok {
				for _, inner := range multi.Unwrap() {
					walk(inner)
				}
				return
			}
			e = errors.Unwrap(e)
		}
	}
	walk(err)
	facts := fmt.Sprintf("refused=%t notServed=%t programReplaced=%t", errors.Is(err, localipc.ErrRefused),
		errors.Is(err, localipc.ErrNotServed), errors.Is(err, localipc.ErrProgramReplaced))
	var peer *localipc.PeerError
	if errors.As(err, &peer) {
		facts += fmt.Sprintf(" peer.pid=%d", peer.PID)
	}
	var refusal *localipc.RefusalError
	if errors.As(err, &refusal) {
		facts += " reason=" + string(refusal.Reason)
	}
	return fmt.Sprintf("%v | types %s | %s", err, strings.Join(types, " > "), facts)
}

func macDiagIdent(info os.FileInfo) string {
	if info == nil {
		return "no file"
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("dev=%d ino=%d mode=%v size=%d mtime=%s", st.Dev, st.Ino, info.Mode(), info.Size(),
			info.ModTime().UTC().Format(time.RFC3339Nano))
	}
	return fmt.Sprintf("mode=%v", info.Mode())
}

// macDiagProgramOf reproduces the kit's programOf step by step and compares the result with this program.
func macDiagProgramOf(pid int) string {
	args, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return fmt.Sprintf("kern.procargs2: %v (%T)", err, err)
	}
	if len(args) < 4 {
		return fmt.Sprintf("kern.procargs2 returned %d bytes", len(args))
	}
	end := bytes.IndexByte(args[4:], 0)
	if end <= 0 {
		return "kern.procargs2 holds no exec path"
	}
	path := string(args[4 : 4+end])
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Sprintf("exec path %q (abs=%t): EvalSymlinks: %v", path, filepath.IsAbs(path), err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Sprintf("exec path %q resolved %q: Stat: %v", path, resolved, err)
	}
	verdict := "same program as the test process"
	if resolved != macDiagOwnPath || !os.SameFile(info, macDiagOwnInfo) {
		verdict = "DIFFERENT program than the test process"
	}
	return fmt.Sprintf("exec path %q (abs=%t) resolved %q regular=%t %s: %s", path, filepath.IsAbs(path), resolved,
		info.Mode().IsRegular(), macDiagIdent(info), verdict)
}

func macDiagPS(pid int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "pid=,ppid=,stat=,etime=,comm=", "-p", fmt.Sprint(pid)).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("ps: %v %q (gone)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func macDiagState(t testing.TB, label string, pid int) {
	t.Helper()
	own := "unchanged"
	if resolved, err := filepath.EvalSymlinks(macDiagOwnPath); err != nil {
		own = "EvalSymlinks: " + err.Error()
	} else if info, err := os.Stat(resolved); err != nil || !os.SameFile(info, macDiagOwnInfo) {
		own = fmt.Sprintf("CHANGED (%v)", err)
	}
	macDiagLog(t, "%s pid %d: ps [%s]; program: %s; own file since start: %s", label, pid, macDiagPS(pid),
		macDiagProgramOf(pid), own)
}

// macDiagDial runs the client's kernel checks with a challenge that sends one line and reads one answer.
func macDiagDial(t testing.TB, label string, e localipc.Endpoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	answer := "(no answer read)"
	start := time.Now()
	conn, err := e.DialWith(ctx, localipc.DialOptions{Server: localipc.CheckHardened,
		Challenge: func(ctx context.Context, conn net.Conn) error {
			if _, err := io.WriteString(conn, "{}\n"); err != nil {
				return err
			}
			line, err := bufio.NewReader(conn).ReadString('\n')
			answer = strings.TrimSpace(line)
			return err
		}})
	if conn != nil {
		_ = conn.Close()
	}
	macDiagLog(t, "%s after %s: err = %s; answer = %q", label, time.Since(start).Round(time.Millisecond),
		macDiagChain(err), answer)
}

func macDiagHolder(t testing.TB, label string, e localipc.Endpoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	pid, err := e.Holder(ctx)
	macDiagLog(t, "%s Holder() = %d, %s", label, pid, macDiagChain(err))
}

// macDiagAccept shows what Accept of a kit listener returns for clients that go away early.
func macDiagAccept(t testing.TB, endpointOf func(string) (localipc.Endpoint, error)) {
	t.Helper()
	e, err := endpointOf(filepath.Join(t.TempDir(), "accept"))
	if err != nil {
		macDiagLog(t, "accept probe: endpoint: %v", err)
		return
	}
	l, err := e.Listen()
	if err != nil {
		macDiagLog(t, "accept probe: Listen: %s", macDiagChain(err))
		return
	}
	path, _ := e.Path()
	lines := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err == nil {
				lines <- "Accept ok"
				_ = conn.Close()
				continue
			}
			lines <- "Accept err: " + macDiagChain(err)
			if errors.Is(err, net.ErrClosed) {
				return
			}
		}
	}()
	for _, how := range []string{"close at once", "write then close", "hold open 200ms"} {
		if conn, err := net.Dial("unix", path); err != nil {
			macDiagLog(t, "accept probe %q: dial: %v", how, err)
		} else {
			switch how {
			case "write then close":
				_, _ = io.WriteString(conn, "x\n")
			case "hold open 200ms":
				time.Sleep(200 * time.Millisecond)
			}
			_ = conn.Close()
		}
		time.Sleep(300 * time.Millisecond)
		for drained := false; !drained; {
			select {
			case line := <-lines:
				macDiagLog(t, "accept probe %q: %s", how, line)
			default:
				drained = true
			}
		}
	}
	_ = l.Close()
	<-done
	for len(lines) > 0 {
		macDiagLog(t, "accept probe at close: %s", <-lines)
	}
}

// macDiagReplaced explains the stand-in process of real, with pid, before and after the test: the kit's checks
// of it, and the same checks against a second stand-in that spawn starts below a vault directory of its own.
func macDiagReplaced(t testing.TB, real localipc.Endpoint, pid int, endpointOf func(string) (localipc.Endpoint, error),
	spawn func(vaultDir string) int) {
	t.Helper()
	path, pathErr := real.Path()
	macDiagLog(t, "test process %d, program %q, %s; real channel %q (%v)", os.Getpid(), macDiagOwnPath,
		macDiagIdent(macDiagOwnInfo), path, pathErr)
	macDiagState(t, "real stand-in before", pid)
	t.Cleanup(func() {
		macDiagState(t, "real stand-in after", pid)
		macDiagHolder(t, "real stand-in after", real)
	})

	dir := filepath.Join(t.TempDir(), "mirror")
	e, err := endpointOf(dir)
	if err != nil {
		macDiagLog(t, "mirror endpoint: %v", err)
		return
	}
	mirror := spawn(dir)
	macDiagState(t, "mirror stand-in", mirror)
	macDiagDial(t, "mirror dial 1", e)
	macDiagState(t, "mirror stand-in after dial 1", mirror)
	macDiagHolder(t, "mirror", e)
	macDiagState(t, "mirror stand-in after Holder", mirror)
	macDiagDial(t, "mirror dial 2", e)
	macDiagState(t, "mirror stand-in after dial 2", mirror)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	ended, endErr := e.EndHolder(ctx)
	cancel()
	macDiagLog(t, "mirror EndHolder() = %d, %s", ended, macDiagChain(endErr))
	time.Sleep(500 * time.Millisecond)
	macDiagState(t, "mirror stand-in after EndHolder", mirror)
	macDiagAccept(t, endpointOf)
}
