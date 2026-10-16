// Strict checks on one JSON line before it is decoded. encoding/json
// matches keys case-insensitively, lets a repeated key win, reads null as
// "absent" and a missing field as zero; the protocol never needs any of
// that, so such lines are refused here. kanshi writes every key in
// lower case (snake_case), never repeats one and never sends null.

package kanshi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// maxDepth bounds JSON nesting; kanshi's deepest value is 4 levels.
const maxDepth = 16

// jsonNode is the shape of a JSON value: the keys of an object (with
// their values' shapes) or the items of an array. Scalars have neither.
type jsonNode struct {
	keys  map[string]*jsonNode
	items []*jsonNode
}

// checkLineBytes refuses a line that is not valid UTF-8 or holds a raw
// control character (C0, DEL or C1), so a line copied to a terminal
// cannot carry escape sequences. Escaped forms such as \u001b are plain
// ASCII in the line and allowed.
func checkLineBytes(line []byte) error {
	if !utf8.Valid(line) {
		return fmt.Errorf("%w: line is not valid UTF-8", ErrProtocol)
	}
	for _, r := range string(line) {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return fmt.Errorf("%w: line holds control character %U", ErrProtocol, r)
		}
	}
	return nil
}

// scanJSON parses line as one JSON object and returns its shape. It
// refuses keys with upper-case letters, repeated keys, null values,
// nesting deeper than maxDepth, and anything after the object.
func scanJSON(line []byte) (*jsonNode, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("%w: line is not a JSON object", ErrProtocol)
	}
	node, err := scanObject(dec, 1)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: data after the JSON object", ErrProtocol)
	}
	return node, nil
}

// scanObject reads an object's members after its '{'.
func scanObject(dec *json.Decoder, depth int) (*jsonNode, error) {
	node := &jsonNode{keys: map[string]*jsonNode{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("%w: object key is not a string", ErrProtocol)
		}
		if strings.ToLower(key) != key {
			return nil, fmt.Errorf("%w: key %q is not lower case", ErrProtocol, Printable(key))
		}
		if _, dup := node.keys[key]; dup {
			return nil, fmt.Errorf("%w: key %q repeated", ErrProtocol, Printable(key))
		}
		value, err := scanValue(dec, depth)
		if err != nil {
			return nil, err
		}
		node.keys[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	return node, nil
}

// scanValue reads one value.
func scanValue(dec *json.Decoder, depth int) (*jsonNode, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: JSON nested deeper than %d", ErrProtocol, maxDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	switch tok {
	case nil:
		return nil, fmt.Errorf("%w: null value", ErrProtocol)
	case json.Delim('{'):
		return scanObject(dec, depth+1)
	case json.Delim('['):
		node := &jsonNode{items: []*jsonNode{}}
		for dec.More() {
			item, err := scanValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			node.items = append(node.items, item)
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
		}
		return node, nil
	}
	return &jsonNode{}, nil
}

// checkRequired reports the first field of t (a struct type) missing
// from node, recursing into nested structs, slices of structs and present
// optional values. A field is optional if it is a pointer or its json tag
// has omitempty; embedded structs contribute their fields.
func checkRequired(node *jsonNode, t reflect.Type, path string) error {
	if node == nil || node.keys == nil {
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if err := checkRequired(node, f.Type, path); err != nil {
				return err
			}
			continue
		}
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		child, present := node.keys[name]
		optional := f.Type.Kind() == reflect.Pointer || opts == "omitempty"
		if !present {
			if optional {
				continue
			}
			return fmt.Errorf("%w: %s%s is missing", ErrProtocol, path, name)
		}
		if err := checkRequiredValue(child, f.Type, path+name); err != nil {
			return err
		}
	}
	return nil
}

// checkRequiredValue applies checkRequired to a struct value, or to each
// struct in a slice, behind any pointers.
func checkRequiredValue(node *jsonNode, t reflect.Type, path string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return checkRequired(node, t, path+".")
	case reflect.Slice:
		elem := t.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			return nil
		}
		for i, item := range node.items {
			if err := checkRequired(item, elem, fmt.Sprintf("%s[%d].", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
