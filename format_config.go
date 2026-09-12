package bcl

import (
	"os"
	"path/filepath"
)

// FormatConfigName is the per-project formatting configuration file. It is itself
// a BCL document, so a project's formatting rules are written in the language
// they format:
//
//	indent 4
//	tabs false
//	keep_blank_lines false
const FormatConfigName = ".bclfmt"

// FindFormatConfig walks up from dir looking for a .bclfmt file, stopping at the
// filesystem root or at a repository boundary. It returns the options it read and
// the file they came from; with no file found it returns the zero options and an
// empty path, which callers read as "use your own defaults".
func FindFormatConfig(dir string) (FormatOptions, string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return FormatOptions{}, "", err
	}
	for {
		candidate := filepath.Join(dir, FormatConfigName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			opts, err := LoadFormatConfig(candidate)
			return opts, candidate, err
		}
		// A repository root bounds the search: formatting is a property of the
		// project, not of whatever happens to be above it on this machine.
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info != nil {
			return FormatOptions{}, "", nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return FormatOptions{}, "", nil
		}
		dir = parent
	}
}

// LoadFormatConfig reads formatting options from a .bclfmt file.
func LoadFormatConfig(path string) (FormatOptions, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return FormatOptions{}, err
	}
	var cfg struct {
		Indent         int  `bcl:"indent"`
		Tabs           bool `bcl:"tabs"`
		KeepBlankLines bool `bcl:"keep_blank_lines"`
	}
	if err := Unmarshal(src, &cfg); err != nil {
		return FormatOptions{}, err
	}
	return FormatOptions{
		IndentWidth:    cfg.Indent,
		UseTabs:        cfg.Tabs,
		KeepBlankLines: cfg.KeepBlankLines,
	}, nil
}
