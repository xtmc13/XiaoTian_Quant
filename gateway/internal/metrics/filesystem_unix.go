//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package metrics

import "syscall"

func filesystemUsage(path string) (totalBytes, availableBytes uint64, err error) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, 0, err
	}
	return stats.Blocks * uint64(stats.Bsize), stats.Bavail * uint64(stats.Bsize), nil
}
