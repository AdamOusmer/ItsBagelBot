// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This guard covers handwritten received-packet code. OS launchers remain in
// pkg/sandbox; the few host capabilities below are fixed startup/broker seams,
// never operations selected by a received event. New dependencies require an
// explicit boundary review rather than silently expanding packet capabilities.
func TestReceivedPacketPackagesCannotAcquireHostCapabilities(t *testing.T) {
	root := filepath.Clean("..")
	for _, directory := range []string{"network", "bridge", "bridge/protocol", "bridge/session", "bridge/transport"} {
		entries, err := fs.ReadDir(os.DirFS(root), directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".pb.go") {
				continue
			}
			file := filepath.Join(directory, name)
			source, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, file), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, violation := range packetBoundaryViolations(filepath.ToSlash(file), source) {
				t.Errorf("%s: %s", file, violation)
			}
		}
	}
}

func packetBoundaryViolations(file string, source *ast.File) []string {
	imports := make(map[string]string, len(source.Imports))
	var violations []string
	for _, item := range source.Imports {
		value, err := strconv.Unquote(item.Path.Value)
		if err != nil {
			violations = append(violations, "invalid import")
			continue
		}
		alias := path.Base(value)
		if item.Name != nil {
			alias = item.Name.Name
		}
		if alias == "." || alias == "_" || !packetImportAllowed(file, value) {
			violations = append(violations, "unreviewed capability import "+value)
		}
		imports[alias] = value
	}
	for _, declaration := range source.Decls {
		function := ""
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			function = fn.Name.Name
		}
		violations = append(violations, packetDeclarationViolations(file, function, declaration, imports)...)
	}
	return violations
}

func packetDeclarationViolations(file, function string, declaration ast.Decl, imports map[string]string) []string {
	var violations []string
	ast.Inspect(declaration, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if packetHostSelector(selector.Sel.Name) {
			violations = append(violations, "host discovery/command selector "+selector.Sel.Name)
		}
		identifier, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if dependency := imports[identifier.Name]; dependency != "" && !packetSelectorAllowed(file, function, dependency, selector.Sel.Name) {
			violations = append(violations, "forbidden capability "+dependency+"."+selector.Sel.Name)
		}
		return true
	})
	return violations
}

var packetPureImports = map[string]bool{
	"context": true, "errors": true, "io": true, "sync": true, "time": true,
	"encoding/binary": true, "math": true, "math/rand/v2": true, "strconv": true,
	"strings": true, "unicode/utf8": true, "crypto/tls": true, "crypto/x509": true,
	"net/url": true, "net/netip": true,
	"ItsBagelBot/app/desktop/internal/bridge/protocol":  true,
	"ItsBagelBot/app/desktop/internal/bridge/session":   true,
	"ItsBagelBot/app/desktop/internal/bridge/transport": true,
	"ItsBagelBot/app/desktop/internal/network/wire":     true,
	"ItsBagelBot/app/desktop/internal/oauth":            true,
	"ItsBagelBot/pkg/protobuf":                          true, "ItsBagelBot/pkg/websocket": true,
	"ItsBagelBot/pkg/sandbox": true,
}
var packetCapabilityImports = map[string]map[string]bool{
	"net":           {"network/broker.go": true, "network/lanes.go": true},
	"os":            {"network/client.go": true, "network/worker.go": true, "network/broker.go": true, "network/lanes.go": true},
	"path/filepath": {"network/client.go": true},
	"runtime":       {"network/client.go": true},
	"net/http":      {"bridge/transport/transport.go": true},
}

func packetImportAllowed(file, dependency string) bool {
	if files, restricted := packetCapabilityImports[dependency]; restricted {
		return files[file]
	}
	return packetPureImports[dependency]
}

func packetSelectorAllowed(file, function, dependency, name string) bool {
	switch dependency {
	case "os":
		return packetOSSelectorAllowed(file, function, name)
	case "runtime":
		return packetRuntimeSelectorAllowed(function, name)
	case "path/filepath":
		return packetPathSelectorAllowed(function, name)
	case "net":
		return packetNetSelectorAllowed(file, function, name)
	case "net/http":
		return packetHTTPSelectorAllowed(name)
	case "ItsBagelBot/pkg/sandbox":
		return packetSandboxSelectorAllowed(file, function, name)
	case "ItsBagelBot/app/desktop/internal/oauth":
		return packetOAuthSelectorAllowed(file, function, name)
	case "ItsBagelBot/pkg/websocket":
		return packetWebSocketSelectorAllowed(file, function, name)
	default:
		return true
	}
}
func packetOSSelectorAllowed(file, function, name string) bool {
	switch name {
	case "File":
		return true
	case "Executable":
		return file == "network/client.go" && function == "HelperPath"
	case "Args":
		return file == "network/worker.go" && function == "RunWorker"
	default:
		return false
	}
}
func packetRuntimeSelectorAllowed(function, name string) bool {
	return function == "HelperPath" && name == "GOOS"
}
func packetPathSelectorAllowed(function, name string) bool {
	return function == "HelperPath" && (name == "Dir" || name == "Clean" || name == "Join")
}
func packetHTTPSelectorAllowed(name string) bool {
	return name == "MethodGet" || name == "MethodPost" || name == "StatusNoContent"
}
func packetOAuthSelectorAllowed(file, function, name string) bool {
	if name == "NewWorkerNetwork" {
		return file == "network/worker.go" && function == "initialize"
	}
	switch name {
	case "NetworkRequest", "NetworkResponse", "TokenReply", "WorkerNetwork", "WorkerNetworkConfig", "ErrUnavailable", "ErrAuthentication", "ErrProtocol":
		return true
	default:
		return false
	}
}
func packetWebSocketSelectorAllowed(file, function, name string) bool {
	if name == "NewClient" {
		return file == "network/worker.go" && function == "initializeBridge" || file == "bridge/transport/direct.go" && function == "NewDirectClient"
	}
	switch name {
	case "Authorization", "Reply", "Client", "Conn", "Config", "ErrAuthentication", "ErrEndpoint", "ErrNonce", "ErrRejected", "ErrTooLarge", "ErrClosed", "ErrDial", "ErrCredentials", "ErrMessageType":
		return true
	default:
		return false
	}
}

func packetNetSelectorAllowed(file, function, name string) bool {
	switch name {
	case "Conn", "Addr", "Error", "ErrClosed", "JoinHostPort":
		return true
	case "DefaultResolver":
		return file == "network/broker.go" && function == "dialPublic"
	case "Dialer":
		return file == "network/broker.go" && function == "dialAddresses"
	default:
		return false
	}
}

func packetSandboxSelectorAllowed(file, function, name string) bool {
	switch name {
	case "Process", "Config":
		return file == "network/client.go"
	case "Start":
		return file == "network/client.go" && function == "newClientForAttempt"
	case "Pipe":
		return file == "network/broker.go" && function == "makeLane"
	case "OpenPipe":
		return file == "network/lanes.go" && function == "parseWorkerFiles"
	case "ChildIsolated", "ControlPipes":
		return file == "network/worker.go" && function == "RunWorker"
	default:
		return false
	}
}

func packetHostSelector(name string) bool {
	switch name {
	case "Interfaces", "InterfaceAddrs", "InterfaceByName", "InterfaceByIndex", "Addrs", "MulticastAddrs", "LocalAddr", "RemoteAddr", "Environ", "Getenv", "LookupEnv", "Command", "CommandContext", "StartProcess", "FindProcess", "Eval", "RunString", "MethodByName", "FieldByName":
		return true
	default:
		return false
	}
}

func TestPacketBoundaryGuardRejectsCapabilityExpansion(t *testing.T) {
	for _, source := range []string{
		`package network; import cli "os/exec"; func receive() { cli.Command("sh") }`,
		`package network; import "net"; func receive() { net.Interfaces() }`,
		`package network; import "net/http"; func receive() { http.Get("https://ip.example") }`,
		`package network; import "os"; func receive() { os.WriteFile("cli", nil, 0) }`,
		`package network; import "reflect"; func receive() { reflect.ValueOf(nil) }`,
		`package network; import "plugin"; func receive() { plugin.Open("remote") }`,
		`package network; import "unsafe"; var p unsafe.Pointer`,
		`package network; import "ItsBagelBot/pkg/credential"; func receive() { credential.New("remote") }`,
		`package network; import "ItsBagelBot/pkg/sandbox"; func receive() { sandbox.Start(nil, sandbox.Config{}) }`,
		`package network; import "net"; func receive() { x := net.Dialer{}; _ = x }`,
		`package network; import . "os"; func receive() { Getenv("SECRET") }`,
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "packet.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, location := range []string{"network/packet.go", "network/broker.go", "network/client.go", "network/worker.go", "network/lanes.go", "bridge/transport/transport.go"} {
			if len(packetBoundaryViolations(location, file)) == 0 {
				t.Fatal("guard accepted host capability expansion", location, source)
			}
		}
	}
}
