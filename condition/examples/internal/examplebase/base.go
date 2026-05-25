package examplebase

import (
	"path/filepath"
	"runtime"
)

func Dir() string {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		return "."
	}
	return filepath.Dir(file)
}
