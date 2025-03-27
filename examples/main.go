package main

import (
	"fmt"
	"os"

	"github.com/oarkflow/bcl"
)

type Database struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	Driver   string `json:"driver"`
}

type Tunnel struct {
	Name       string   `json:"name"`
	Host       string   `json:"host"`
	LocalPort  int      `json:"local_port"`
	RemotePort int      `json:"remote_port"`
	Enabled    bool     `json:"enabled"`
	Statuses   []int    `json:"statuses"`
	Extras     Extras   `json:"extras"`
	Database   Database `json:"database"`
}

type Extras struct {
	MaxLatency float64 `json:"max_latency"`
}

type Credential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Config struct {
	LocalBasePort int        `json:"local_port_base"`
	Credential    Credential `json:"credential"`
	Tunnels       []Tunnel   `json:"tunnel"`
	DefaultTunnel Tunnel     `json:"defaultTunnel"`
	DefaultHost   string     `json:"defaultHost"`
}

func main() {
	// Set an environment variable for interpolation.
	_ = os.Setenv("APP_NAME", "dev")

	// Read configuration from file "config.bcl".
	cfg, err := os.ReadFile("workflow.bcl")
	if err != nil {
		panic(err)
	}

	var config map[string]any
	ast, err := bcl.Unmarshal(cfg, &config)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshaled configuration from struct:")
	fmt.Printf("%+v\n", config)

	marshaled := bcl.MarshalAST(ast)
	fmt.Println("\nMarshaled configuration from struct:")
	fmt.Println(string(marshaled))
}
