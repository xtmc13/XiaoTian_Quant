package ml

import "runtime"

func defaultMLPythonExecutable() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}
