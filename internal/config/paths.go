package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultAddr = "127.0.0.1:9229"

type Paths struct {
	Home      string
	TokenFile string
	PidFile   string
	LogFile   string
	MediaDir  string
	MacrosDir string
}

func DefaultPaths() Paths {
	home := os.Getenv("CHROME_CTL_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			home = ".chrome-ctl"
		} else {
			home = filepath.Join(userHome, ".chrome-ctl")
		}
	}
	return Paths{
		Home:      home,
		TokenFile: filepath.Join(home, "auth_token"),
		PidFile:   filepath.Join(home, "daemon.pid"),
		LogFile:   filepath.Join(home, "daemon.log"),
		MediaDir:  filepath.Join(home, "media"),
		MacrosDir: filepath.Join(home, "macros"),
	}
}

func (p Paths) Ensure() error {
	if err := os.MkdirAll(p.Home, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return os.Chmod(p.Home, 0o700)
}

func DefaultAddr() string {
	if addr := os.Getenv("CHROME_CTL_ADDR"); addr != "" {
		return addr
	}
	return defaultAddr
}

func WritePID(p Paths, pid int) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	return os.WriteFile(p.PidFile, []byte(strconv.Itoa(pid)), 0o600)
}

func ReadPID(p Paths) (int, bool, error) {
	data, err := os.ReadFile(p.PidFile)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false, nil
	}
	return pid, true, nil
}

func RemovePID(p Paths) error {
	err := os.Remove(p.PidFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
