package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
)

type Config struct {
	Password     string            `json:"password"`
	Programs     map[string]string `json:"programs"`               // name -> absolute path
	BaseGamePath string            `json:"baseGamePath,omitempty"` // e.g., C:/Program Files/Steam/steamapps/common/garrysmod
	WinePath     string            `json:"winePath,omitempty"`     // optional override for wine binary
	// Optional overrides for variable expansion. if empty, values are derived.
	GameDir string `json:"gamedir,omitempty"`
	ExeDir  string `json:"exedir,omitempty"`
	BspDir  string `json:"bspdir,omitempty"`
	TmpDir  string `json:"tmp,omitempty"`
}

var (
	config     Config
	configOnce sync.Once
)

func LoadConfig(path string) (Config, error) {
	var err error
	configOnce.Do(func() {
		conf, e := readConfig(path)
		if e != nil {
			def := Config{Password: "", Programs: map[string]string{}}
			_ = writeConfig(path, def)
			conf = def
		}

		config = conf
		err = nil
	})

	return config, err
}

func readConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}

	if c.Programs == nil {
		c.Programs = map[string]string{}
	}

	// Derive default program paths from BaseGamePath if provided.
	deriveDefaultPrograms(&c)
	return c, nil
}

func writeConfig(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, b, fs.FileMode(0644)); err != nil {
		return err
	}

	return nil
}

func deriveDefaultPrograms(c *Config) {
	if c.BaseGamePath == "" {
		return
	}

	base := c.BaseGamePath
	base = strings.ReplaceAll(base, "\\", "/")

	ensure := func(key, rel string) {
		if c.Programs[key] == "" {
			c.Programs[key] = base + rel
		}
	}

	ensure("vbsp", "/bin/win64/vbsp.exe")
	ensure("vvis", "/bin/win64/vvisplusplus.exe")
	ensure("vrad", "/bin/win64/vrad.exe")
}

func CheckPassword(provided string) error {
	if config.Password == "" {
		return nil
	}

	if provided != config.Password {
		return errors.New("unauthorized")
	}

	return nil
}
