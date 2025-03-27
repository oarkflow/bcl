package bcl

import (
	"encoding/json"
	"testing"
)

type Tunnel struct {
	Name       string `json:"Name"`
	Host       string `json:"host"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
	Enabled    bool   `json:"enabled"`
	Extras     struct {
		MaxLatency float64 `json:"max_latency"`
	} `json:"extras"`
}

// Sample BCL configuration used for BCL benchmarks.
// This is similar to your earlier sample.
var bclConfig = []byte(`@include "credentials.bcl"
domain = username+"@acme.com"
appName = "${env.APP_NAME:'DEV PROD'}"
default_port = 8400
local_port_base = default_port + 1000

tunnel "myservice-prod1" {
	host = appName + "." + domain
	local_port = local_port_base + 1
	remote_port = default_port
	enabled = true

	extras {
		max_latency = 8.5
	}
}

tunnel "myservice-prod2" {
	host = appName + "." + domain
	local_port = local_port_base + 2
	remote_port = default_port
	enabled = true

	def extras {
		max_latency = 9.0
	}
}
`)

var cfg = []byte(`@include "bcl_credentials.bcl"
var domain = username+"@acme.com"
var appName = "{{env.APP_NAME:'dev'}}"
var default_port    = 8400
var local_port_base = default_port + 1000

def tunnel "myservice-prod1" {
	host = appName + "." + domain
	local_port  = local_port_base + 1
	remote_port = default_port
	enabled = true

	def extras {
		max_latency = 8.5 # [ms]
	}
}

def tunnel "myservice-prod2" {
	host = appName + "." + domain
	local_port  = local_port_base + 1
	remote_port = default_port
	enabled = true

	def extras {
		max_latency = 8.5 # [ms]
	}
}
`)

// Sample JSON configuration for comparison. It uses the same fields as the Tunnel struct.
var jsonConfig = []byte(`[
  {
    "Name": "myservice-prod1",
    "host": "AcmeApp.acme.com",
    "local_port": 9401,
    "remote_port": 8400,
    "enabled": true,
    "extras": {
      "max_latency": 8.5
    }
  },
  {
    "Name": "myservice-prod2",
    "host": "AcmeApp.acme.com",
    "local_port": 9402,
    "remote_port": 8400,
    "enabled": true,
    "extras": {
      "max_latency": 9.0
    }
  }
]`)

// BenchmarkBCLMarshalUnmarshal benchmarks round-trip processing of BCL configuration using the AST functions.
func BenchmarkCustomMarshalUnmarshal(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var ast []map[string]any
		err := Unmarshal(bclConfig, &ast)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJSONMarshalUnmarshal benchmarks round-trip processing using JSON marshal/unmarshal.
func BenchmarkJSONMarshalUnmarshal(b *testing.B) {
	var tunnels []Tunnel
	for i := 0; i < b.N; i++ {
		err := json.Unmarshal(jsonConfig, &tunnels)
		if err != nil {
			b.Fatal(err)
		}
	}
}
