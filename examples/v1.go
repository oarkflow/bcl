package main

import (
	"fmt"

	"github.com/oarkflow/bcl"
)

func main() {
	var input = `
appName = "Boilerplate", version = 1.2, minVersion = (version <= 1.2 || version > 5) ? "dev":"prod"
credentials = @include "credentials.bcl"
@include "https://raw.githubusercontent.com/github-linguist/linguist/refs/heads/main/samples/HCL/example.hcl"
server main {
    host   = "localhost"
    port   = 8080
    secure = false
}
server "main1 server" {
    host   = "localhost"
    port   = 8080
    secure = false
	settings = {
		debug     = true
		timeout   = 30
		rateLimit = 100
	}
}
settings = {
    debug     = true
    timeout   = 30
    rateLimit = 100
}
users = ["alice", "bob", "charlie"]
permissions = [
	{
		user   = "alice"
		access = "full"
	}
	{
		user   = "bob"
		access = "read-only"
	}
]
ten = 10
calc = ten + 5
test = isDefined(eleven) ? eleven : ["alice", "bob", "charlie"]
defaultUser = credentials.username
defaultHost = server."main".host
defaultServer = server."main1 server"
fallbackServer = server.main
// ---- New dynamic expression examples ----
greeting = "Welcome to ${upper(appName)}"
dynamicCalc = "The sum is ${calc}"
// ---- New examples for unary operator expressions ----
negNumber = -10
notTrue = !true
doubleNeg = -(-5)
negCalc = -calc
// ---- New examples for env lookup ----
envHome = "${appName}_HOME"
defaultShell = "${env.SHELL:/bin/bash}"
IF (settings.debug) {
    logLevel = "verbose"
} ELSE {
    logLevel = "normal"
}
	// Fix heredoc: Add an extra newline after the <<EOF marker.
	line = <<EOF
This is # test.
yet another test
EOF

Link "nodeA" -> "nodeB" {
    label  = "Edge from A to B"
    weight = 100
}
	`

	var cfg map[string]any
	nodes, err := bcl.Unmarshal([]byte(input), &cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshalled Config:")
	fmt.Printf("%+v\n\n", cfg)

	str := bcl.MarshalAST(nodes)
	fmt.Println("Marshaled AST:")
	fmt.Println(str)
}
