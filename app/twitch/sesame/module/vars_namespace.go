// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import "ItsBagelBot/pkg/tmpl"

// Namespace identifies the owner of public module reply fields.
type Namespace string

func TokenKey(tok tmpl.Token, namespace Namespace) string {
	return tmpl.TokenKey(tok, string(namespace))
}

func (t TokenExpander[R]) ExpandNamespaced(namespace Namespace, text string, r *R) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if field, ok := t[TokenKey(tok, namespace)]; ok {
			return field(r), true
		}
		return pureFallback(tok)
	})
}

func (p StringPalette) ExpandNamespaced(namespace Namespace, text string) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if val, ok := p[TokenKey(tok, namespace)]; ok {
			return val, true
		}
		return pureFallback(tok)
	})
}

// WithNamespace accepts both public {module:field} tokens and legacy {field} tokens.
func (p Palette) WithNamespace(namespace Namespace) Palette { p.namespace = namespace; return p }
