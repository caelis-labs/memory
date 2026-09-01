//go:build windows

package appliance

import (
	"os"

	"golang.org/x/sys/windows"
)

func secureOwnerPath(path string, _ os.FileMode) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	aceFlags := ""
	if info, statErr := os.Stat(path); statErr != nil {
		return statErr
	} else if info.IsDir() {
		aceFlags = "OICI"
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;" + aceFlags + ";FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}
