package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"

	"github.com/oarkflow/bcl"
)

type Config struct {
	Profiles []PythonProfile `bcl:"profile,block"`
}

type PythonProfile struct {
	Name            string           `bcl:",id"`
	Kind            string           `bcl:"kind,ident"`
	Runtime         string           `bcl:"runtime,ident"`
	DependencyMode  string           `bcl:"dependency_mode,ident"`
	DetectFiles     []string         `bcl:"detect_files"`
	StartCommand    []string         `bcl:"start_command"`
	StartupTimeout  int              `bcl:"startup_timeout"`
	ShutdownTimeout int              `bcl:"shutdown_timeout"`
	HealthPath      string           `bcl:"health_path"`
	Commands        []ProfileCommand `bcl:"command,block"`
	Env             map[string]any   `bcl:"env"`
}

type ProfileCommand struct {
	Name    string   `bcl:",id"`
	Phase   string   `bcl:"phase,ident"`
	Command []string `bcl:"exec"`
	Timeout int      `bcl:"timeout"`
}

func main() {
	input := filepath.Join(exampleDir(), "..", "python.bcl")
	source, err := os.ReadFile(input)
	if err != nil {
		log.Fatal(err)
	}

	var cfg Config
	if err := bcl.Unmarshal(source, &cfg); err != nil {
		log.Fatal(err)
	}

	encoded, err := bcl.Marshal(cfg)
	if err != nil {
		log.Fatal(err)
	}

	var roundTrip Config
	if err := bcl.Unmarshal(encoded, &roundTrip); err != nil {
		log.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, roundTrip) {
		log.Fatalf("round trip changed config:\noriginal: %#v\nround trip: %#v", cfg, roundTrip)
	}

	output := filepath.Join(os.TempDir(), "bcl-python-profile.bcl")
	if err := os.WriteFile(output, encoded, 0644); err != nil {
		log.Fatal(err)
	}

	for _, profile := range cfg.Profiles {
		fmt.Printf("decoded profile %q: runtime=%s commands=%d env=%d\n",
			profile.Name, profile.Runtime, len(profile.Commands), len(profile.Env))
	}
	fmt.Printf("\nencoded %s:\n%s", output, encoded)
}

func exampleDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("cannot locate example directory")
	}
	return filepath.Dir(file)
}
