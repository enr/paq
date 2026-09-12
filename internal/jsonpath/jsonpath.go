// Package jsonpath selects a value inside a decoded JSON document with a
// dot-separated path. It covers the projects that publish a piece of release
// metadata (a checksum, a version) only through an API.
package jsonpath

import (
	"fmt"
	"strconv"
	"strings"
)

// Select walks doc following a dot-separated path (e.g. "sha256hash",
// "assets.0.digest"). A segment indexes an array when the current node is one
// and the segment is a number, and is a key otherwise.
func Select(doc any, selector string) (any, error) {
	if selector == "" {
		return nil, fmt.Errorf("empty JSON selector")
	}
	node := doc
	for _, seg := range strings.Split(selector, ".") {
		switch n := node.(type) {
		case map[string]any:
			v, ok := n[seg]
			if !ok {
				return nil, fmt.Errorf("no key %q at %q", seg, selector)
			}
			node = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("segment %q of %q is not an array index", seg, selector)
			}
			if i < 0 || i >= len(n) {
				return nil, fmt.Errorf("index %d of %q is out of range (array has %d elements)", i, selector, len(n))
			}
			node = n[i]
		default:
			return nil, fmt.Errorf("segment %q of %q applies to %T, want an object or an array", seg, selector, node)
		}
	}
	return node, nil
}

// String is Select with the selected value asserted to be a string.
func String(doc any, selector string) (string, error) {
	v, err := Select(doc, selector)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("value at %q is %T, want a string", selector, v)
	}
	return s, nil
}
