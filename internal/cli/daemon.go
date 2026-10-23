package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/buildinfo"
	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/rajeshshrirao/chrome-ctl/internal/daemon"
	"github.com/spf13/cobra"
)

const (
	startTimeout = 3 * time.Second
	stopTimeout  = 5 * time.Second
	pollInterval = 50 * time.Millisecond
)

func newDaemonCmd() *cobra.Command {
	var start, stop, foreground bool
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run or manage the local daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths := config.DefaultPaths()
			addr := config.DefaultAddr()
			switch {
			case start:
				return startDaemon(cmd, addr, paths)
			case stop:
				return stopDaemon(cmd, addr, paths)
			default:
				return runForeground(cmd, addr, paths)
			}
		},
	}
	cmd.Flags().BoolVar(&start, "start", false, "Start the daemon in the background")
	cmd.Flags().BoolVar(&stop, "stop", false, "Stop the running daemon")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run the daemon in the foreground")
	cmd.MarkFlagsMutuallyExclusive("start", "stop")
	_ = cmd.Flags().MarkHidden("foreground")
	return cmd
}

func runForeground(cmd *cobra.Command, addr string, paths config.Paths) error {
	out := cmd.OutOrStdout()
	if err := paths.Ensure(); err != nil {
		return err
	}
	token, created, err := config.EnsureToken(paths.TokenFile)
	if err != nil {
		return err
	}

	logger := log.New(cmd.ErrOrStderr(), "", log.LstdFlags)
	if pid, ok, err := config.ReadPID(paths); err != nil {
		return fmt.Errorf("read pidfile: %w", err)
	} else if ok && pid != os.Getpid() && processAlive(pid) {
		return fmt.Errorf("daemon already running (pid %d)", pid)
	}
	if err := config.WritePID(paths, os.Getpid()); err != nil {
		return err
	}
	defer config.RemovePID(paths)

	fmt.Fprintf(out, "Daemon: LISTENING (%s)\n", addr)
	if created {
		fmt.Fprintf(out, "Auth token: %s\n", token)
	}
	fmt.Fprintf(out, "Token file: %s\n", paths.TokenFile)

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	return daemon.Run(ctx, daemon.Config{
		Addr:    addr,
		Version: buildinfo.Version,
		Paths:   paths,
		Log:     logger,
	})
}

func startDaemon(cmd *cobra.Command, addr string, paths config.Paths) error {
	out := cmd.OutOrStdout()
	if err := paths.Ensure(); err != nil {
		return err
	}
	token, created, err := config.EnsureToken(paths.TokenFile)
	if err != nil {
		return err
	}
	if checkHealth(addr, token) {
		fmt.Fprintf(out, "Daemon: ALREADY RUNNING (%s)\n", addr)
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	logFile, err := os.OpenFile(paths.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	child := exec.Command(exe, "daemon", "--foreground")
	child.Stdout = logFile
	child.Stderr = logFile
	child.Env = os.Environ()
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	childPID := child.Process.Pid
	if err := child.Process.Release(); err != nil {
		return fmt.Errorf("detach daemon: %w", err)
	}

	if !waitHealthy(addr, token, startTimeout) {
		return fmt.Errorf("daemon did not become healthy within %s; check %s", startTimeout, paths.LogFile)
	}

	pid := childPID
	if pidFromFile, ok, err := config.ReadPID(paths); err == nil && ok {
		pid = pidFromFile
	}
	fmt.Fprintf(out, "Daemon: STARTED (pid %d, %s)\n", pid, addr)
	if created {
		fmt.Fprintf(out, "Auth token: %s\n", token)
	}
	fmt.Fprintf(out, "Token file: %s\n", paths.TokenFile)
	return nil
}

func stopDaemon(cmd *cobra.Command, addr string, paths config.Paths) error {
	out := cmd.OutOrStdout()
	pid, ok, err := config.ReadPID(paths)
	if err != nil {
		return fmt.Errorf("read pidfile: %w", err)
	}
	if !ok || !processAlive(pid) {
		if err := config.RemovePID(paths); err != nil {
			return err
		}
		fmt.Fprintf(out, "Daemon: NOT RUNNING (%s)\n", addr)
		return nil
	}

	token, err := config.ReadToken(paths.TokenFile)
	if err != nil {
		return fmt.Errorf("daemon pid %d is alive but its auth token is unreadable; refusing to signal it", pid)
	}
	if !checkHealth(addr, token) {
		return fmt.Errorf("daemon pid %d is alive but not responding on %s; refusing to signal a possibly reused pid", pid, addr)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			if err := config.RemovePID(paths); err != nil {
				return err
			}
			fmt.Fprintf(out, "Daemon: NOT RUNNING (%s)\n", addr)
			return nil
		}
		return fmt.Errorf("stop daemon (pid %d): %w", pid, err)
	}
	if !waitStopped(pid, stopTimeout) {
		return fmt.Errorf("daemon (pid %d) did not stop within %s", pid, stopTimeout)
	}
	if err := config.RemovePID(paths); err != nil {
		return err
	}
	fmt.Fprintf(out, "Daemon: STOPPED (%s)\n", addr)
	return nil
}

func healthCheck(ctx context.Context, addr, token string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode == http.StatusOK
}

func checkHealth(addr, token string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return healthCheck(ctx, addr, token)
}

func waitHealthy(addr, token string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if checkHealth(addr, token) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}

func waitStopped(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
