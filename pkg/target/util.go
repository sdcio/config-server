/*
Copyright 2024 Nokia.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package target

import (
	"errors"
	"sort"
	"strings"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
)

var errMalformedXPath = errors.New("malformed xpath")
var errMalformedXPathKey = errors.New("malformed xpath key")

var escapedBracketsReplacer = strings.NewReplacer(`\]`, `]`, `\[`, `[`)

func ParsePath(p string) (*sdcpb.Path, error) {
	lp := len(p)
	if lp == 0 {
		return &sdcpb.Path{}, nil
	}
	var origin string

	idx := strings.Index(p, ":")
	if idx >= 0 && p[0] != '/' && !strings.Contains(p[:idx], "/") &&
		// path == origin:/ || path == origin:
		((idx+1 < lp && p[idx+1] == '/') || (lp == idx+1)) {
		origin = p[:idx]
		p = p[idx+1:]
	}

	pes, err := toPathElems(p)
	if err != nil {
		return nil, err
	}
	return &sdcpb.Path{
		Origin: origin,
		Elem:   pes,
	}, nil
}

// toPathElems parses a xpath and returns a list of path elements
func toPathElems(p string) ([]*sdcpb.PathElem, error) {
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	buffer := make([]rune, 0)
	null := rune(0)
	prevC := rune(0)
	// track if the loop is traversing a key
	inKey := false
	for _, r := range p {
		switch r {
		case '[':
			if inKey && prevC != '\\' {
				return nil, errMalformedXPath
			}
			if prevC != '\\' {
				inKey = true
			}
		case ']':
			if !inKey && prevC != '\\' {
				return nil, errMalformedXPath
			}
			if prevC != '\\' {
				inKey = false
			}
		case '/':
			if !inKey {
				buffer = append(buffer, null)
				prevC = r
				continue
			}
		}
		buffer = append(buffer, r)
		prevC = r
	}
	if inKey {
		return nil, errMalformedXPath
	}
	stringElems := strings.Split(string(buffer), string(null))
	pElems := make([]*sdcpb.PathElem, 0, len(stringElems))
	for _, s := range stringElems {
		if s == "" {
			continue
		}
		pe, err := toPathElem(s)
		if err != nil {
			return nil, err
		}
		pElems = append(pElems, pe)
	}
	return pElems, nil
}

// toPathElem take a xpath formatted path element such as "elem1[k=v]" and returns the corresponding mgmt_server.PathElem
func toPathElem(s string) (*sdcpb.PathElem, error) {
	idx := -1
	prevC := rune(0)
	for i, r := range s {
		if r == '[' && prevC != '\\' {
			idx = i
			break
		}
		prevC = r
	}
	var kvs map[string]string
	if idx > 0 {
		var err error
		kvs, err = parseXPathKeys(s[idx:])
		if err != nil {
			return nil, err
		}
		s = s[:idx]
	}
	return &sdcpb.PathElem{Name: s, Key: kvs}, nil
}

// parseXPathKeys takes keys definition from an xpath, e.g [k1=v1][k2=v2] and return the keys and values as a map[string]string
func parseXPathKeys(s string) (map[string]string, error) {
	if len(s) == 0 {
		return nil, nil
	}
	kvs := make(map[string]string)
	inKey := false
	start := 0
	prevRune := rune(0)
	for i, r := range s {
		switch r {
		case '[':
			if prevRune == '\\' {
				prevRune = r
				continue
			}
			if inKey {
				return nil, errMalformedXPathKey
			}
			inKey = true
			start = i + 1
		case ']':
			if prevRune == '\\' {
				prevRune = r
				continue
			}
			if !inKey {
				return nil, errMalformedXPathKey
			}
			eq := strings.Index(s[start:i], "=")
			if eq < 0 {
				return nil, errMalformedXPathKey
			}
			k, v := s[start:i][:eq], s[start:i][eq+1:]
			if len(k) == 0 || len(v) == 0 {
				return nil, errMalformedXPathKey
			}
			kvs[escapedBracketsReplacer.Replace(k)] = escapedBracketsReplacer.Replace(v)
			inKey = false
		}
		prevRune = r
	}
	if inKey {
		return nil, errMalformedXPathKey
	}
	return kvs, nil
}

///////////

// ToStrings converts gnmi.Path to index strings. When index strings are generated,
// gnmi.Path will be irreversibly lost. Index strings will be built by using name field
// in gnmi.PathElem. If gnmi.PathElem has key field, values will be included in
// alphabetical order of the keys.
// E.g. <target>/<origin>/a/b[b:d, a:c]/e will be returned as <target>/<origin>/a/b/c/d/e
// If prefix parameter is set to true, <target> and <origin> fields of
// the gnmi.Path will be prepended in the index strings unless they are empty string.
// gnmi.Path.Element field is deprecated, but being gracefully handled by this function
// in the absence of gnmi.Path.Elem.
func ToStrings(p *sdcpb.Path, prefix, nokeys bool) []string {
	is := []string{}
	if p == nil {
		return is
	}
	if prefix {
		// add target to the list of index strings
		if t := p.GetTarget(); t != "" {
			is = append(is, t)
		}
		// add origin to the list of index strings
		if o := p.GetOrigin(); o != "" {
			is = append(is, o)
		}
	}
	for _, e := range p.GetElem() {
		is = append(is, e.GetName())
		if !nokeys {
			is = append(is, sortedVals(e.GetKey())...)
		}
	}

	return is
}

func sortedVals(m map[string]string) []string {
	// Special case single key lists.
	if len(m) == 1 {
		for _, v := range m {
			return []string{v}
		}
	}
	// Return deterministic ordering of multi-key lists.
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	vs := make([]string, 0, len(m))
	for _, k := range ks {
		vs = append(vs, m[k])
	}
	return vs
}

func CompletePath(prefix, path *sdcpb.Path) ([]string, error) {
	oPre, oPath := prefix.GetOrigin(), path.GetOrigin()

	var fullPrefix []string
	indexedPrefix := ToStrings(prefix, false, false)
	switch {
	case oPre != "" && oPath != "":
		return nil, errors.New("origin is set both in prefix and path")
	case oPre != "":
		fullPrefix = append(fullPrefix, oPre)
		fullPrefix = append(fullPrefix, indexedPrefix...)
	case oPath != "":
		if len(indexedPrefix) > 0 {
			return nil, errors.New("path elements in prefix are set even though origin is set in path")
		}
		fullPrefix = append(fullPrefix, oPath)
	default:
		// Neither prefix nor path specified an origin. Include the path elements in prefix.
		fullPrefix = append(fullPrefix, indexedPrefix...)
	}

	return append(fullPrefix, ToStrings(path, false, false)...), nil
}

func ToXPath(p *sdcpb.Path, noKeys bool) string {
	if p == nil {
		return ""
	}
	sb := strings.Builder{}
	if p.Origin != "" {
		sb.WriteString(p.Origin)
		sb.WriteString(":")
	}
	elems := p.GetElem()
	numElems := len(elems)
	for i, pe := range elems {
		sb.WriteString(pe.GetName())
		if !noKeys {
			for k, v := range pe.GetKey() {
				sb.WriteString("[")
				sb.WriteString(k)
				sb.WriteString("=")
				sb.WriteString(v)
				sb.WriteString("]")
			}
		}
		if i+1 != numElems {
			sb.WriteString("/")
		}
	}
	return sb.String()
}
