// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const module = "ItsBagelBot/"

var rawNATSMethods = map[string]bool{
	"Request": true, "RequestWithContext": true, "RequestMsg": true, "RequestMsgWithContext": true,
	"Publish": true, "PublishMsg": true, "PublishRequest": true,
	"Subscribe": true, "SubscribeSync": true, "ChanSubscribe": true,
	"QueueSubscribe": true, "QueueSubscribeSync": true, "ChanQueueSubscribe": true,
}

var rawNATSAllowed = map[string]string{
	"ItsBagelBot/app/clusterops/internal/natsclaims":    "system-account claim updates carry no message",
	"ItsBagelBot/internal/natsmigrate":                  "operator stream migration tool",
	"ItsBagelBot/internal/domain/invalidate":            "cache invalidation fan-out",
	"ItsBagelBot/app/projector":                         "cache invalidation fan-out",
	"ItsBagelBot/app/projector/rpc":                     "tier invalidation listener",
	"ItsBagelBot/app/twitch/outgress/internal/channels": "pause invalidation listener",
	"ItsBagelBot/app/db/users/repository":               "device revocation broadcast",
	"ItsBagelBot/pkg/ratelimit":                         "pod-addressed permit borrow, timed by its own segment",
}

var clientAllowed = map[string]string{
	"ItsBagelBot/app/release/internal/gh": "release CLI, no service traffic",
}

var bannedHTTP = map[string]bool{
	"net/http.Get": true, "net/http.Head": true, "net/http.Post": true, "net/http.PostForm": true,
	"net/http.DefaultClient": true,
	"github.com/newrelic/go-agent/v3/newrelic.StartExternalSegment": true,
	"github.com/newrelic/go-agent/v3/newrelic.NewRoundTripper":      true,
}

type finding struct {
	pkg, rule string
}

func TestEveryServiceCallIsTraced(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  "../..",
	}, "./app/...", "./pkg/...", "./internal/...")
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, pkg := range pkgs {
		for _, f := range scanPackage(pkg) {
			violations = append(violations, f.pkg+": "+f.rule)
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("untraced calls; route them through pkg/bus or monitor.ExternalTransport:\n%s", strings.Join(violations, "\n"))
	}
}

func scanPackage(pkg *packages.Package) []finding {
	if !strings.HasPrefix(pkg.PkgPath, module) || ownsTelemetry(pkg.PkgPath) || pkg.TypesInfo == nil {
		return nil
	}
	var found []finding
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			if rule := violation(pkg, node); rule != "" {
				found = append(found, finding{pkg: pkg.Fset.Position(node.Pos()).String(), rule: rule})
			}
			return true
		})
	}
	return found
}

func ownsTelemetry(path string) bool {
	return path == module+"pkg/monitor" || path == module+"pkg/bus" || strings.HasPrefix(path, module+"pkg/bus/")
}

func violation(pkg *packages.Package, node ast.Node) string {
	switch n := node.(type) {
	case *ast.SelectorExpr:
		return selectorViolation(pkg, n)
	case *ast.CompositeLit:
		if clientAllowed[pkg.PkgPath] != "" {
			return ""
		}
		return clientViolation(pkg.TypesInfo, n)
	}
	return ""
}

func selectorViolation(pkg *packages.Package, sel *ast.SelectorExpr) string {
	obj := pkg.TypesInfo.Uses[sel.Sel]
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	if packageLevel(obj) && bannedHTTP[obj.Pkg().Path()+"."+obj.Name()] {
		return obj.Pkg().Name() + "." + obj.Name()
	}
	if rawNATSCall(pkg.TypesInfo, sel) && rawNATSAllowed[pkg.PkgPath] == "" {
		return "raw nats." + sel.Sel.Name
	}
	return ""
}

func packageLevel(obj types.Object) bool {
	return obj.Parent() == obj.Pkg().Scope()
}

func rawNATSCall(info *types.Info, sel *ast.SelectorExpr) bool {
	if !rawNATSMethods[sel.Sel.Name] {
		return false
	}
	recv := info.TypeOf(sel.X)
	return recv != nil && strings.TrimPrefix(recv.String(), "*") == "github.com/nats-io/nats.go.Conn"
}

func clientViolation(info *types.Info, lit *ast.CompositeLit) string {
	if t := info.TypeOf(lit); t == nil || t.String() != "net/http.Client" {
		return ""
	}
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok && isKey(kv, "Transport") {
			return transportViolation(info, kv.Value)
		}
	}
	return "http.Client without monitor.ExternalTransport"
}

func isKey(kv *ast.KeyValueExpr, name string) bool {
	ident, ok := kv.Key.(*ast.Ident)
	return ok && ident.Name == name
}

func transportViolation(info *types.Info, value ast.Expr) string {
	call, ok := value.(*ast.CallExpr)
	if !ok {
		return fmt.Sprintf("http.Client transport %T is not monitor.ExternalTransport", value)
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if ok && objectPath(info, sel) == module+"pkg/monitor.ExternalTransport" {
		return ""
	}
	return "http.Client transport is not monitor.ExternalTransport"
}

func objectPath(info *types.Info, sel *ast.SelectorExpr) string {
	obj := info.Uses[sel.Sel]
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path() + "." + obj.Name()
}
