package componentmetrics

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// A subprocess tests real OS signals without modifying the test runner's handlers.
func TestSecondSignalTerminatesDrain(t *testing.T) {
	if os.Getenv("GOPULSE_SIGNAL_TEST_CHILD") == "1" {
		ctx, stop := SignalContext()
		defer stop()
		fmt.Println("listening")
		<-ctx.Done()
		stop() // Wait until the first signal's handler has been unregistered.
		fmt.Println("draining")
		time.Sleep(30 * time.Second)
		os.Exit(9)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSecondSignalTerminatesDrain$")
	cmd.Env = append(os.Environ(), "GOPULSE_SIGNAL_TEST_CHILD=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	lines := make(chan string, 2)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			lines <- s.Text()
		}
	}()
	await := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("signal transition timed out")
		}
	}
	await("listening")
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	await("draining")
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("second signal unexpectedly exited zero")
		}
		status := cmd.ProcessState.Sys().(syscall.WaitStatus)
		if !status.Signaled() || status.Signal() != syscall.SIGTERM {
			t.Fatalf("unexpected exit: %v", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second signal swallowed during drain")
	}
}
