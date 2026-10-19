package runner

// A prefix's wineserver lives in the pid namespace of the Flatpak instance
// that started it, and every instance has its own: a menu launcher's, the
// GUI's after a restart. Instances share /tmp, so a wine started in another
// instance finds the server's socket and joins it, but the server knows its
// clients by the pids they give and signals them by pid, and a foreign pid
// names some other process here: `cellar ps` from a restarted GUI ended an
// app's virtual desktop, and the app with it. So a wine command whose
// server is foreign is handed to an agent in the server's instance: each
// `cellar run` serves one while its server is up (Serve), and Command runs
// such commands as `cellar wine-proxy` (Proxy), which hands them over.

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// proxyNoAgent is Proxy's exit code when no agent took the command.
const proxyNoAgent = 125

// ProxyCommand is the hidden cellar command Command runs for a foreign
// server: `cellar wine-proxy <wine> args...`, with the prefix in WINEPREFIX.
const ProxyCommand = "wine-proxy"

type owner int

const (
	noServer owner = iota
	localServer
	foreignServer
)

// serverDir is the prefix's wineserver directory, as Wine names it:
// /tmp/.wine-<uid>/server-<dev>-<inode>; "" for a prefix that is not there.
func serverDir(prefix string) string {
	var st syscall.Stat_t
	if syscall.Stat(prefix, &st) != nil {
		return ""
	}
	return filepath.Join("/tmp", fmt.Sprintf(".wine-%d", os.Getuid()), fmt.Sprintf("server-%x-%x", st.Dev, st.Ino))
}

// serverOwner tells whose namespace the prefix's wineserver is in. The
// server holds a write lock on its lock file; the kernel gives the holder's
// pid as this namespace sees it, 0 for one out of its sight.
func serverOwner(prefix string) owner {
	dir := serverDir(prefix)
	if dir == "" {
		return noServer
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR, 0)
	if err != nil {
		return noServer
	}
	defer f.Close()
	lk := syscall.Flock_t{Type: syscall.F_WRLCK}
	if syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lk) != nil || lk.Type == syscall.F_UNLCK {
		return noServer
	}
	if lk.Pid == 0 {
		return foreignServer
	}
	// A parent namespace (the host) sees the pid of a server in a child.
	theirs, err1 := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", lk.Pid))
	ours, err2 := os.Readlink("/proc/self/ns/pid")
	if err1 == nil && err2 == nil && theirs != ours {
		return foreignServer
	}
	return localServer
}

// Frames on an agent connection: a kind byte, a length, the payload.
const (
	frameStdout = 'o'
	frameStderr = 'e'
	frameExit   = 'x' // int32 exit code
	frameRefuse = 'n' // the server is not this agent's (any more)
	frameSignal = 's' // client to agent: one byte, the signal number
)

type proxyRequest struct {
	Argv []string `json:"argv"`
	Env  []string `json:"env"`
	Dir  string   `json:"dir"`
}

func writeFrame(w io.Writer, kind byte, p []byte) error {
	h := make([]byte, 5, 5+len(p))
	h[0] = kind
	binary.BigEndian.PutUint32(h[1:], uint32(len(p)))
	_, err := w.Write(append(h, p...))
	return err
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var h [5]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	p := make([]byte, binary.BigEndian.Uint32(h[1:]))
	_, err := io.ReadFull(r, p)
	return h[0], p, err
}

// frameWriter sends a stream as frames. A client gone is not the command's
// problem: it swallows the error so the command never blocks on its output.
type frameWriter struct {
	mu   *sync.Mutex
	conn net.Conn
	kind byte
}

func (w frameWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	writeFrame(w.conn, w.kind, p)
	return len(p), nil
}

// Serve runs an agent for the prefix's wineserver: once the server is up
// and ours, it takes wine commands from other instances on a socket in the
// server's directory. The returned done stops it; done(true) first waits
// for the server to go, so programs the run leaves behind stay manageable
// from other instances. With the server foreign it serves nothing.
func Serve(prefix string) (done func(linger bool)) {
	stop := make(chan struct{})
	var (
		mu sync.Mutex
		ln net.Listener
	)
	go func() {
		for {
			if serverOwner(prefix) == localServer {
				var b [4]byte
				rand.Read(b[:])
				l, err := net.Listen("unix", filepath.Join(serverDir(prefix), "cellar-"+hex.EncodeToString(b[:])+".sock"))
				if err != nil {
					return
				}
				mu.Lock()
				select {
				case <-stop:
					mu.Unlock()
					l.Close()
					return
				default:
					ln = l
				}
				mu.Unlock()
				for {
					c, err := l.Accept()
					if err != nil {
						return
					}
					go serveConn(c, prefix)
				}
			}
			select {
			case <-stop:
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
	var once sync.Once
	return func(linger bool) {
		once.Do(func() {
			mu.Lock()
			serving := ln != nil
			mu.Unlock()
			for linger && serving && serverOwner(prefix) == localServer {
				time.Sleep(time.Second)
			}
			mu.Lock()
			close(stop)
			if ln != nil {
				ln.Close() // a unix listener removes its socket file
			}
			mu.Unlock()
		})
	}
}

func serveConn(c net.Conn, prefix string) {
	defer c.Close()
	br := bufio.NewReader(c)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return
	}
	var req proxyRequest
	if json.Unmarshal(line, &req) != nil || len(req.Argv) == 0 {
		return
	}
	if serverOwner(prefix) != localServer {
		writeFrame(c, frameRefuse, nil)
		return
	}
	var mu sync.Mutex
	cmd := exec.Command(req.Argv[0], req.Argv[1:]...)
	cmd.Env, cmd.Dir = req.Env, req.Dir
	cmd.Stdout = frameWriter{&mu, c, frameStdout}
	cmd.Stderr = frameWriter{&mu, c, frameStderr}
	// Programs it starts inherit its output; past its own exit, stop
	// waiting for theirs.
	cmd.WaitDelay = 2 * time.Second
	code := 0
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(cmd.Stderr, "cellar agent: %v\n", err)
		code = 127
	} else {
		go func() {
			for {
				kind, p, err := readFrame(br)
				if err != nil {
					return
				}
				if kind == frameSignal && len(p) == 1 {
					cmd.Process.Signal(syscall.Signal(p[0]))
				}
			}
		}()
		cmd.Wait()
		code = cmd.ProcessState.ExitCode()
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			code = 128 + int(ws.Signal())
		}
	}
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(int32(code)))
	mu.Lock()
	writeFrame(c, frameExit, b[:])
	mu.Unlock()
}

// ErrNoAgent is what a wine command for a server in another instance
// fails with when no agent there takes it.
var ErrNoAgent = errors.New("this prefix's Wine runs in another cellar instance (a menu launcher, or a window since closed) that takes no requests; end its programs there, or wait for them to exit")

// Proxy is `cellar wine-proxy`: it runs argv through an agent of the
// server WINEPREFIX names, relaying output, signals and the exit code.
func Proxy(argv []string) int {
	if len(argv) == 0 {
		return 2
	}
	dir, _ := os.Getwd()
	req, _ := json.Marshal(proxyRequest{Argv: argv, Env: os.Environ(), Dir: dir})
	socks, _ := filepath.Glob(filepath.Join(serverDir(os.Getenv("WINEPREFIX")), "cellar-*.sock"))
	for _, s := range socks {
		c, err := net.Dial("unix", s)
		if err != nil {
			if errors.Is(err, syscall.ECONNREFUSED) {
				os.Remove(s) // its agent died without cleaning up
			}
			continue
		}
		if code, ok := proxyOn(c, req); ok {
			return code
		}
	}
	fmt.Fprintln(os.Stderr, "cellar:", ErrNoAgent)
	return proxyNoAgent
}

// proxyOn runs the request on one agent; false if it refused.
func proxyOn(c net.Conn, req []byte) (int, bool) {
	defer c.Close()
	if _, err := c.Write(append(req, '\n')); err != nil {
		return 0, false
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	go func() {
		for s := range sigs {
			writeFrame(c, frameSignal, []byte{byte(s.(syscall.Signal))})
		}
	}()
	for {
		kind, p, err := readFrame(c)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cellar: the agent running this command went away")
			return 125, true
		}
		switch kind {
		case frameStdout:
			os.Stdout.Write(p)
		case frameStderr:
			os.Stderr.Write(p)
		case frameRefuse:
			return 0, false
		case frameExit:
			if len(p) == 4 {
				return int(int32(binary.BigEndian.Uint32(p))), true
			}
			return 125, true
		}
	}
}
