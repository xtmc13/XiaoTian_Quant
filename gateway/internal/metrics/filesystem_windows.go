//go:build windows

package metrics

import "golang.org/x/sys/windows"

func filesystemUsage(path string) (totalBytes, availableBytes uint64, err error) {
	widePath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(widePath, &availableBytes, &totalBytes, nil); err != nil {
		return 0, 0, err
	}
	return totalBytes, availableBytes, nil
}
