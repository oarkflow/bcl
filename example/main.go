package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/oarkflow/bcl"
)

type AppConfig struct {
	Name     string `bcl:"name"`
	Enabled  bool   `bcl:"enabled"`
	Workers  int    `bcl:"workers"`
	CacheTTL string `bcl:"cache_ttl"`
}

func main() {
	env := func(key string) (string, bool) {
		values := map[string]string{
			"DATABASE_URL":       "postgres://example/processgate",
			"TLS_KEY":            "/etc/processgate/tls.key",
			"WORKERS":            "12",
			"APP_ENV":            "prod",
			"DB_NAME":            "processgate",
			"DB_USER":            "processgate",
			"IDENTITY_API_TOKEN": "example-token",
		}
		v, ok := values[key]
		if ok {
			return v, true
		}
		return os.LookupEnv(key)
	}

	compiled, err := bcl.CompileFile("example/main.bcl", &bcl.Options{
		AllowEnv:       true,
		ResolveImports: true,
		ResolveModules: true,
		Profile:        "prod",
		Env:            env,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("== Compiled main.bcl ==")
	printJSON(compiled)

	appSource, err := os.ReadFile("example/app.bcl")
	if err != nil {
		log.Fatal(err)
	}

	var app AppConfig
	if err := bcl.Unmarshal(appSource, &app); err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n== Decoded app.bcl into Go struct ==")
	fmt.Printf("%+v\n", app)

	encoded, err := bcl.Marshal(app)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n== Marshaled Go struct back to BCL ==")
	fmt.Print(string(encoded))

	ok, err := bcl.Eval(`subject.roles has_any ["admin", "superadmin"]`, map[string]any{
		"subject": map[string]any{
			"roles": []any{"member", "admin"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n== Expression evaluation ==")
	fmt.Printf("admin role matched: %v\n", ok)
}

func printJSON(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
}
