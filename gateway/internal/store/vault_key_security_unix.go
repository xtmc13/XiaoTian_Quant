//go:build !windows

package store

import (
	"fmt"
	"os"
)

func secureVaultKeyFile(path string) error {
	return os.Chmod(path, 0o600)
}

func checkVaultKeyFileSecure(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("key file mode is %04o, want 0600", info.Mode().Perm())
	}
	return nil
}
