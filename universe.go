package virtuakube

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var universeTools = []string{
	"vde_switch",
	"qemu-system-x86_64",
	"qemu-img",
}

// checkTools returns an error if a command required by virtuakube is
// not available on the system.
func checkTools(tools []string) error {
	missing := []string{}
	for _, tool := range tools {
		_, err := exec.LookPath(tool)
		if err != nil {
			if e, ok := err.(*exec.Error); ok && e.Err == exec.ErrNotFound {
				missing = append(missing, tool)
				continue
			}
			return err
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required tools missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

// A Universe is a virtual test network and its associated resources.
type Universe struct {
	tmpdir   string
	ctx      context.Context
	shutdown context.CancelFunc
	ports    chan int
	nextIP4  net.IP
	nextIP6  net.IP
	vms      map[string]*VM

	swtch *exec.Cmd
	sock  string

	closeMu  sync.Mutex
	closed   bool
	closeErr error
}

// New creates a new virtual universe. The ctx controls the overall
// lifetime of the universe, i.e. if the context is canceled or times
// out, the universe will be destroyed.
func New(ctx context.Context) (*Universe, error) {
	if err := checkTools(universeTools); err != nil {
		return nil, err
	}

	p, err := ioutil.TempDir("", "virtuakube")
	if err != nil {
		return nil, err
	}

	ctx, shutdown := context.WithCancel(ctx)

	sock := filepath.Join(p, "switch")

	ret := &Universe{
		tmpdir:   p,
		ctx:      ctx,
		shutdown: shutdown,
		ports:    make(chan int),
		nextIP4:  net.ParseIP("172.20.0.1").To4(),
		nextIP6:  net.ParseIP("fd00::1"),
		vms:      map[string]*VM{},
		swtch: exec.CommandContext(
			ctx,
			"vde_switch",
			"--sock", sock,
			"-m", "0600",
		),
		sock: sock,
	}

	if err := ret.swtch.Start(); err != nil {
		ret.Close()
		return nil, err
	}
	// Destroy the universe if the virtual switch exits
	go func() {
		ret.swtch.Wait()
		// TODO: logging and stuff
		ret.Close()
	}()
	// Destroy the universe if the parent context cancels
	go func() {
		<-ctx.Done()
		ret.Close()
	}()
	go func() {
		port := 50000
		for {
			select {
			case ret.ports <- port:
				port++
			case <-ctx.Done():
				return
			}
		}
	}()

	return ret, nil
}

// Tmpdir creates a temporary directory and returns its absolute
// path. The directory will be cleaned up when the universe is
// destroyed.
func (u *Universe) Tmpdir(prefix string) (string, error) {
	p, err := ioutil.TempDir(u.tmpdir, prefix)
	if err != nil {
		return "", err
	}
	return p, nil
}

// Context returns a context that gets canceled when the universe is
// destroyed.
func (u *Universe) Context() context.Context {
	return u.ctx
}

// Close destroys the universe, freeing up processes and temporary
// files.
func (u *Universe) Close() error {
	u.closeMu.Lock()
	defer u.closeMu.Unlock()
	if u.closed {
		return u.closeErr
	}
	u.closed = true

	u.shutdown()

	u.closeErr = os.RemoveAll(u.tmpdir)
	return u.closeErr
}

// Wait waits for the universe to end.
func (u *Universe) Wait(ctx context.Context) error {
	select {
	case <-u.ctx.Done():
		return nil
	case <-ctx.Done():
		return errors.New("timeout")
	}
}

func (u *Universe) VM(hostname string) *VM {
	return u.vms[hostname]
}

func (u *Universe) switchSock() string {
	return u.sock
}

func (u *Universe) ipv4() net.IP {
	ret := u.nextIP4
	u.nextIP4 = make(net.IP, 4)
	copy(u.nextIP4, ret)
	u.nextIP4[3]++
	return ret
}

func (u *Universe) ipv6() net.IP {
	ret := u.nextIP6
	u.nextIP6 = make(net.IP, 16)
	copy(u.nextIP6, ret)
	u.nextIP6[15]++
	return ret
}
