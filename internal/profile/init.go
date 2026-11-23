package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/mazen160/backlog/internal/config"
)

// LockInit serializes init commands from the collision check through registration.
func LockInit() (*os.File, error) {
	dir := config.GlobalDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "init.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func CheckInitPath(name, path string) error {
	g, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return checkInitPath(g, name, path)
}

func checkInitPath(g *config.GlobalConfig, name, path string) error {
	p, exists := g.Profiles[name]
	if !exists {
		return nil
	}

	current := config.ExpandPath(p.Path)
	currentAbs, err := filepath.Abs(current)
	if err != nil {
		return err
	}
	if currentAbs == path {
		return nil
	}
	if currentInfo, err := os.Stat(currentAbs); err == nil {
		if targetInfo, err := os.Stat(path); err == nil && os.SameFile(currentInfo, targetInfo) {
			return nil
		}
	}
	return fmt.Errorf("profile %q already points to %q, use a different --profile name", name, current)
}

// RegisterInit publishes the completed workspace and active profile together.
func RegisterInit(name, path string, setDefault bool) (bool, error) {
	g, err := config.LoadGlobal()
	if err != nil {
		return false, err
	}
	if err := checkInitPath(g, name, path); err != nil {
		return false, err
	}
	if _, exists := g.Profiles[name]; !exists {
		g.Profiles[name] = config.Profile{Path: path}
	} else if !setDefault && g.DefaultProfile != "" {
		return false, nil
	}
	isDefault := setDefault || g.DefaultProfile == ""
	if isDefault {
		g.DefaultProfile = name
	}
	return isDefault, saveInitRegistry(g)
}

func saveInitRegistry(g *config.GlobalConfig) error {
	path := filepath.Join(config.GlobalDir(), "config.toml")
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if info != nil && info.Mode()&os.ModeSymlink != 0 {
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	var saveErr error
	if info != nil {
		saveErr = file.Chmod(info.Mode().Perm())
	}
	if saveErr == nil {
		saveErr = toml.NewEncoder(file).Encode(g)
	}
	if err := errors.Join(saveErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
