package bcl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test data for benchmarks
var (
	smallBCL = `
name = "test"
version = 1.0
enabled = true
`

	mediumBCL = `
@include "credentials.bcl"
name = "MyApp"
version = 2.5
debug = true

server "main" {
	host = "localhost"
	port = 8080
	ssl = false

	database {
		driver = "postgres"
		host = "localhost"
		port = 5432
		name = "myapp"
		user = "dbuser"
		password = "${env.DB_PASS:secret}"
	}
}

features = {
	auth = true
	api = true
	websocket = false
}

users = [
	{ name = "alice", role = "admin" }
	{ name = "bob", role = "user" }
	{ name = "charlie", role = "user" }
]
`

	largeBCL string
)

func init() {
	// Generate large BCL content
	var sb strings.Builder
	sb.WriteString("config = {\n")
	for i := 0; i < 1000; i++ {
		sb.WriteString(fmt.Sprintf(`  item%d = {
    name = "Item %d"
    value = %d
    enabled = %v
    tags = ["tag1", "tag2", "tag3"]
  }
`, i, i, i*10, i%2 == 0))
	}
	sb.WriteString("}\n")
	largeBCL = sb.String()
}

// Benchmark parsing performance
func BenchmarkParse(b *testing.B) {
	tests := []struct {
		name string
		data string
	}{
		{"Small", smallBCL},
		{"Medium", mediumBCL},
		{"Large", largeBCL},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				parser := NewParser(tt.data)
				_, err := parser.Parse()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Benchmark evaluation performance
func BenchmarkEval(b *testing.B) {
	parser := NewParser(mediumBCL)
	nodes, err := parser.Parse()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := NewEnv(nil)
		for _, node := range nodes {
			_, err := node.Eval(env)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Benchmark string builder pool
func BenchmarkStringBuilderPool(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sb := getBuilder(64)
			sb.WriteString("test")
			sb.WriteString("data")
			sb.WriteString("here")
			_ = sb.String()
			putBuilder(sb)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var sb strings.Builder
			sb.Grow(64)
			sb.WriteString("test")
			sb.WriteString("data")
			sb.WriteString("here")
			_ = sb.String()
		}
	})
}

// Benchmark concurrent parsing
func BenchmarkConcurrentParsing(b *testing.B) {
	// Create temporary files
	tmpDir := b.TempDir()
	files := make([]string, 10)
	for i := 0; i < 10; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("test%d.bcl", i))
		if err := os.WriteFile(filename, []byte(mediumBCL), 0644); err != nil {
			b.Fatal(err)
		}
		files[i] = filename
	}

	b.Run("Sequential", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil {
					b.Fatal(err)
				}
				parser := NewParser(string(data))
				nodes, err := parser.Parse()
				if err != nil {
					b.Fatal(err)
				}
				env := NewEnv(nil)
				for _, node := range nodes {
					_, err := node.Eval(env)
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		}
	})

	b.Run("Concurrent", func(b *testing.B) {
		cp := NewConcurrentParser(4)
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := cp.ParseAndMerge(ctx, files)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Benchmark JSON conversion
func BenchmarkJSONConversion(b *testing.B) {
	var config map[string]any
	_, err := Unmarshal([]byte(mediumBCL), &config)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("BCLToJSON", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := MarshalJSON(config)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("StandardJSON", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := json.Marshal(config)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Benchmark function registry
func BenchmarkFunctionRegistry(b *testing.B) {
	// Register some test functions
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("testFunc%d", i)
		RegisterFunction(name, func(args ...any) (any, error) {
			return nil, nil
		})
	}

	b.Run("Lookup", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = LookupFunction("testFunc50")
		}
	})

	b.Run("Register", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			name := fmt.Sprintf("benchFunc%d", i)
			RegisterFunction(name, func(args ...any) (any, error) {
				return nil, nil
			})
		}
	})
}

// Benchmark interpolation
func BenchmarkInterpolation(b *testing.B) {
	env := NewEnv(nil)
	env.vars["name"] = "TestApp"
	env.vars["version"] = "1.2.3"
	env.vars["count"] = 42

	tests := []struct {
		name string
		str  string
	}{
		{"NoInterpolation", "This is a plain string"},
		{"SingleVar", "App: ${name}"},
		{"MultipleVars", "App: ${name} v${version} (${count} items)"},
		{"Expression", "Total: ${count * 2}"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := interpolateDynamic(tt.str, env)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Benchmark memory allocations
func BenchmarkMemoryAllocations(b *testing.B) {
	b.Run("ParseAndEval", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			parser := NewParser(mediumBCL)
			nodes, err := parser.Parse()
			if err != nil {
				b.Fatal(err)
			}
			env := NewEnv(nil)
			for _, node := range nodes {
				_, err := node.Eval(env)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// Benchmark large file parsing
func BenchmarkLargeFileParsing(b *testing.B) {
	// Generate a very large BCL file
	var sb strings.Builder
	for i := 0; i < 10000; i++ {
		sb.WriteString(fmt.Sprintf(`
server "server%d" {
	host = "host%d.example.com"
	port = %d
	enabled = %v

	config {
		timeout = %d
		retries = %d
		buffer = %d
	}
}
`, i, i, 8000+i, i%2 == 0, 30+i%10, i%5, 1024*(i%8+1)))
	}

	largeBCLContent := sb.String()

	b.Run("Parse", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			parser := NewParser(largeBCLContent)
			_, err := parser.Parse()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Benchmark comparison with JSON
func BenchmarkBCLvsJSON(b *testing.B) {
	// BCL data
	bclData := []byte(mediumBCL)

	// Equivalent JSON data
	jsonData := []byte(`{
		"name": "MyApp",
		"version": 2.5,
		"debug": true,
		"server": {
			"main": {
				"host": "localhost",
				"port": 8080,
				"ssl": false,
				"database": {
					"driver": "postgres",
					"host": "localhost",
					"port": 5432,
					"name": "myapp",
					"user": "dbuser",
					"password": "secret"
				}
			}
		},
		"features": {
			"auth": true,
			"api": true,
			"websocket": false
		},
		"users": [
			{"name": "alice", "role": "admin"},
			{"name": "bob", "role": "user"},
			{"name": "charlie", "role": "user"}
		]
	}`)

	b.Run("BCL/Unmarshal", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var result map[string]any
			_, err := Unmarshal(bclData, &result)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("JSON/Unmarshal", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var result map[string]any
			err := json.Unmarshal(jsonData, &result)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Benchmark caching effectiveness
func BenchmarkIncludeCaching(b *testing.B) {
	// Create a test file
	tmpDir := b.TempDir()
	includeFile := filepath.Join(tmpDir, "include.bcl")
	if err := os.WriteFile(includeFile, []byte(`shared_value = 42`), 0644); err != nil {
		b.Fatal(err)
	}

	mainBCL := fmt.Sprintf(`@include "%s"
value = shared_value * 2
`, includeFile)

	b.Run("WithCache", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			parser := NewParser(mainBCL)
			nodes, err := parser.Parse()
			if err != nil {
				b.Fatal(err)
			}
			env := NewEnv(nil)
			for _, node := range nodes {
				_, err := node.Eval(env)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("WithoutCache", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Clear cache before each iteration
			globalIncludeCache.Clear()

			parser := NewParser(mainBCL)
			nodes, err := parser.Parse()
			if err != nil {
				b.Fatal(err)
			}
			env := NewEnv(nil)
			for _, node := range nodes {
				_, err := node.Eval(env)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// Benchmark expression evaluation
func BenchmarkExpressionEvaluation(b *testing.B) {
	env := NewEnv(nil)
	env.vars["a"] = 10
	env.vars["b"] = 20
	env.vars["c"] = 30
	env.vars["str"] = "hello"

	expressions := []struct {
		name string
		expr string
	}{
		{"Simple", "a + b"},
		{"Complex", "(a + b) * c / 2"},
		{"String", `str + " world"`},
		{"Function", "upper(str)"},
		{"Ternary", "a > b ? a : b"},
		{"Nested", "a + (b * (c - 10))"},
	}

	for _, tt := range expressions {
		b.Run(tt.name, func(b *testing.B) {
			parser := NewParser(tt.expr)
			node, err := parser.parseExpression()
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := node.Eval(env)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Benchmark real-world scenario
func BenchmarkRealWorldScenario(b *testing.B) {
	// Simulate a real configuration file
	realWorldBCL := `
# Application Configuration
app_name = "ProductionApp"
environment = "${env.APP_ENV:production}"
version = "3.2.1"
debug = environment != "production"

# Server Configuration
server "api" {
	host = "0.0.0.0"
	port = ${env.PORT:8080}
	workers = 4

	ssl {
		enabled = true
		cert = "/etc/ssl/cert.pem"
		key = "/etc/ssl/key.pem"
	}

	limits {
		max_connections = 1000
		request_timeout = 30
		body_size = 10485760  # 10MB
	}
}

# Database Configuration
database "primary" {
	driver = "postgresql"
	host = "${env.DB_HOST:localhost}"
	port = ${env.DB_PORT:5432}
	name = "${env.DB_NAME:myapp}"
	user = "${env.DB_USER:postgres}"
	password = "${env.DB_PASS}"

	pool {
		min = 5
		max = 20
		idle_timeout = 300
	}
}

# Cache Configuration
cache "redis" {
	host = "${env.REDIS_HOST:localhost}"
	port = ${env.REDIS_PORT:6379}
	db = 0
	password = "${env.REDIS_PASS:}"

	options {
		connect_timeout = 5
		read_timeout = 3
		write_timeout = 3
	}
}

# Feature Flags
features = {
	new_ui = true
	beta_api = false
	analytics = true
	rate_limiting = true
}

# Logging Configuration
logging {
	level = debug ? "debug" : "info"
	format = "json"

	outputs = [
		{
			type = "console"
			enabled = true
		}
		{
			type = "file"
			enabled = environment == "production"
			path = "/var/log/app.log"
			rotate = true
			max_size = 104857600  # 100MB
		}
	]
}

# Service Discovery
services = [
	{
		name = "auth-service"
		url = "http://auth.internal:8001"
		timeout = 10
	}
	{
		name = "user-service"
		url = "http://users.internal:8002"
		timeout = 10
	}
	{
		name = "notification-service"
		url = "http://notify.internal:8003"
		timeout = 5
	}
]
`

	b.Run("FullParse", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var config map[string]any
			_, err := Unmarshal([]byte(realWorldBCL), &config)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ToJSON", func(b *testing.B) {
		var config map[string]any
		_, err := Unmarshal([]byte(realWorldBCL), &config)
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := MarshalJSON(config)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Helper to create test credentials.bcl
func createTestCredentials(b *testing.B) string {
	tmpDir := b.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.bcl")
	content := `username = "testuser"
password = "testpass"`
	if err := os.WriteFile(credFile, []byte(content), 0644); err != nil {
		b.Fatal(err)
	}
	return tmpDir
}
