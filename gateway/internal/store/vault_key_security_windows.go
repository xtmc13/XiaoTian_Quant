//go:build windows

package store

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const vaultKeySecurityDescriptor = "D:P(A;;FA;;;OW)(A;;FA;;;SY)"

func secureVaultKeyFile(path string) error {
	sd, err := windows.SecurityDescriptorFromString(vaultKeySecurityDescriptor)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

func checkVaultKeyFileSecure(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if dacl == nil || dacl.AceCount != 2 || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("vault key DACL is not restricted to its owner and SYSTEM")
	}
	return nil
}
