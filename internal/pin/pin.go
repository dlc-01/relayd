package pin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Save(pinFile, fingerprint string) error {
	if err := os.MkdirAll(filepath.Dir(pinFile), 0700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return os.WriteFile(pinFile, []byte(fingerprint), 0600)
}

func Load(pinFile string) (string, error) {
	data, err := os.ReadFile(pinFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func Exists(pinFile string) bool {
	_, err := os.Stat(pinFile)
	return err == nil
}
