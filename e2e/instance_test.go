//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type instance struct {
	name    string
	baseURL string
	cmd     *exec.Cmd
	logs    *syncBuffer
	exited  chan error
}

var started struct {
	mu        sync.Mutex
	instances []*instance
}

func startInstance(name string) (*instance, error) {
	addr, err := freeAddr()
	if err != nil {
		return nil, err
	}

	inst := &instance{
		name:    name,
		baseURL: "http://" + addr,
		logs:    &syncBuffer{},
		exited:  make(chan error, 1),
	}
	inst.cmd = exec.Command(suite.bin, "run",
		"-config", suite.configPath,
		"-http-addr", addr,
		"-kafka-brokers", strings.Join(suite.brokers, ","),
		"-kafka-tls", "off",
		"-auth", "off",
		"-http-tls", "off",
	)
	inst.cmd.Env = append(os.Environ(), "RISK_DB_DSN="+suite.dbDSN)
	inst.cmd.Stdout = inst.logs
	inst.cmd.Stderr = inst.logs

	started.mu.Lock()
	started.instances = append(started.instances, inst)
	started.mu.Unlock()

	if err := inst.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	go func() { inst.exited <- inst.cmd.Wait() }()

	if err := inst.waitReady(30 * time.Second); err != nil {
		inst.kill()
		return nil, err
	}
	return inst, nil
}

func (i *instance) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err, exited := i.exitErr(); exited {
			return fmt.Errorf("%s exited before ready: %v", i.name, err)
		}
		if i.readyStatus() == http.StatusOK {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s not ready after %s", i.name, timeout)
}

// exitErr reports whether the process has exited, leaving the result for later callers.
func (i *instance) exitErr() (error, bool) {
	select {
	case err := <-i.exited:
		i.exited <- err
		return err, true
	default:
		return nil, false
	}
}

func (i *instance) readyStatus() int {
	resp, err := http.Get(i.baseURL + "/v1/readycheck")
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (i *instance) getJSON(path string, v any) (int, error) {
	resp, err := http.Get(i.baseURL + path)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(v)
}

// stop sends SIGTERM and returns the exit error, nil for a clean exit.
func (i *instance) stop() error {
	if err, exited := i.exitErr(); exited {
		return err
	}
	if err := i.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case err := <-i.exited:
		i.exited <- err
		return err
	case <-time.After(30 * time.Second):
		i.kill()
		return fmt.Errorf("%s did not exit within 30s of SIGTERM", i.name)
	}
}

func (i *instance) kill() {
	i.cmd.Process.Kill()
	err := <-i.exited
	i.exited <- err
}

func stopInstances() {
	for _, inst := range suite.instances {
		if err := inst.stop(); err != nil {
			fmt.Fprintf(os.Stderr, "stop %s: %v\n", inst.name, err)
		}
	}
}

func dumpLogs() {
	started.mu.Lock()
	defer started.mu.Unlock()
	for _, inst := range started.instances {
		fmt.Fprintf(os.Stderr, "\n===== %s logs =====\n%s", inst.name, inst.logs.String())
	}
}

func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
