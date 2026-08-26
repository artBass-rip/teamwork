package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Paths struct {
	Config  string
	Data    string
	State   string
	Runtime string
	Modules string
	Socket  string
	DB      string
}

func Resolve() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	var config, data, state, run string
	switch runtime.GOOS {
	case "darwin":
		data = filepath.Join(home, "Library", "Application Support", "TeamWork")
		config = data
		state = filepath.Join(home, "Library", "Logs", "TeamWork")
		run = filepath.Join(os.TempDir(), fmt.Sprintf("teamwork-%d", os.Getuid()))
	case "linux":
		config = envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		data = envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
		state = envOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
		run = os.Getenv("XDG_RUNTIME_DIR")
		if run == "" {
			run = filepath.Join(os.TempDir(), fmt.Sprintf("teamwork-%d", os.Getuid()))
		}
		config, data, state = filepath.Join(config, "teamwork"), filepath.Join(data, "teamwork"), filepath.Join(state, "teamwork")
	default:
		return Paths{}, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
	if override := os.Getenv("TEAMWORK_HOME"); override != "" {
		config, data, state, run = override, override, override, filepath.Join(override, "run")
	}
	modules := filepath.Join(data, "modules")
	if override := os.Getenv("TEAMWORK_MODULES_DIR"); override != "" {
		modules = override
	} else if executable, executableErr := os.Executable(); executableErr == nil {
		bundled := filepath.Join(filepath.Dir(executable), "modules")
		if info, statErr := os.Stat(bundled); statErr == nil && info.IsDir() {
			modules = bundled
		}
	}
	return Paths{Config: config, Data: data, State: state, Runtime: run, Modules: modules, Socket: filepath.Join(run, "core.sock"), DB: filepath.Join(data, "core.db")}, nil
}

func (p Paths) Prepare() error {
	for _, path := range []string{p.Config, p.Data, p.State, p.Runtime, p.Modules} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
