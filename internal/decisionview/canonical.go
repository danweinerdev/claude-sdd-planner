package decisionview

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"unicode/utf8"
)

const CanonicalVersion = "entry-v1"

var canonicalIntegerRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
var canonicalNumberType = reflect.TypeOf(json.Number(""))

// EncodeEntry implements the versioned typed encoding, not JSON presentation.
// Input is the parsed entry itself; ledger metadata/location never enter it.
func EncodeEntry(version string, entry map[string]any) ([]byte, error) {
	if version != CanonicalVersion {
		return nil, fmt.Errorf("decisionview: unsupported canonical version %q", version)
	}
	if entry == nil {
		return nil, fmt.Errorf("decisionview: canonical entry must be an object")
	}
	var out bytes.Buffer
	if err := encodeCanonicalValue(&out, reflect.ValueOf(entry), 0); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func DecodeEntry(version string, data []byte) (map[string]any, error) {
	if version != CanonicalVersion {
		return nil, fmt.Errorf("decisionview: unsupported canonical version %q", version)
	}
	p := canonicalParser{data: data}
	value, err := p.value(0)
	if err != nil {
		return nil, err
	}
	if p.pos != len(data) {
		return nil, fmt.Errorf("decisionview: trailing canonical bytes")
	}
	entry, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("decisionview: canonical entry must be an object")
	}
	return entry, nil
}
func DigestEntry(version string, entry map[string]any) (string, error) {
	encoded, err := EncodeEntry(version, entry)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte("sdd-decision-entry-v1\x00"))
	h.Write(encoded)
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

func canonicalString(out *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("decisionview: canonical strings must be UTF-8")
	}
	out.WriteByte('s')
	out.WriteString(strconv.Itoa(len(s)))
	out.WriteByte(':')
	out.WriteString(s)
	return nil
}
func encodeCanonicalValue(out *bytes.Buffer, v reflect.Value, depth int) error {
	if depth > 64 {
		return fmt.Errorf("decisionview: canonical nesting exceeds 64 (or input is cyclic)")
	}
	if !v.IsValid() {
		out.WriteByte('n')
		return nil
	}
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			out.WriteByte('n')
			return nil
		}
		v = v.Elem()
	}
	if v.Type() == canonicalNumberType {
		s := v.String()
		if !canonicalIntegerRe.MatchString(s) {
			return fmt.Errorf("decisionview: unsupported canonical number %q", s)
		}
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return fmt.Errorf("decisionview: invalid integer")
		}
		out.WriteByte('i')
		out.WriteString(n.String())
		out.WriteByte(';')
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		return canonicalString(out, v.String())
	case reflect.Bool:
		if v.Bool() {
			out.WriteByte('t')
		} else {
			out.WriteByte('f')
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out.WriteByte('i')
		out.WriteString(strconv.FormatInt(v.Int(), 10))
		out.WriteByte(';')
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		out.WriteByte('i')
		out.WriteString(strconv.FormatUint(v.Uint(), 10))
		out.WriteByte(';')
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("decisionview: canonical object keys must be strings")
		}
		if v.IsNil() {
			out.WriteByte('n')
			return nil
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		out.WriteByte('o')
		out.WriteString(strconv.Itoa(len(keys)))
		out.WriteByte(':')
		for _, key := range keys {
			if err := canonicalString(out, key.String()); err != nil {
				return err
			}
			if err := encodeCanonicalValue(out, v.MapIndex(key), depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return fmt.Errorf("decisionview: binary data is not a canonical array")
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			out.WriteByte('n')
			return nil
		}
		out.WriteByte('a')
		out.WriteString(strconv.Itoa(v.Len()))
		out.WriteByte(':')
		for i := 0; i < v.Len(); i++ {
			if err := encodeCanonicalValue(out, v.Index(i), depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("decisionview: unsupported canonical value type %s", v.Type())
	}
	return nil
}

type canonicalParser struct {
	data []byte
	pos  int
}

func (p *canonicalParser) count() (int, error) {
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}
	if start == p.pos || p.pos == len(p.data) || p.data[p.pos] != ':' {
		return 0, fmt.Errorf("decisionview: invalid canonical length")
	}
	digits := p.data[start:p.pos]
	p.pos++
	if len(digits) > 1 && digits[0] == '0' {
		return 0, fmt.Errorf("decisionview: non-minimal canonical length")
	}
	n, err := strconv.Atoi(string(digits))
	if err != nil || n < 0 || n > len(p.data)-p.pos {
		return 0, fmt.Errorf("decisionview: truncated or oversized canonical length")
	}
	return n, nil
}
func (p *canonicalParser) value(depth int) (any, error) {
	if depth > 64 || p.pos >= len(p.data) {
		return nil, fmt.Errorf("decisionview: truncated or deeply nested canonical value")
	}
	tag := p.data[p.pos]
	p.pos++
	switch tag {
	case 'n':
		return nil, nil
	case 't':
		return true, nil
	case 'f':
		return false, nil
	case 's':
		n, err := p.count()
		if err != nil {
			return nil, err
		}
		s := string(p.data[p.pos : p.pos+n])
		p.pos += n
		if !utf8.ValidString(s) {
			return nil, fmt.Errorf("decisionview: invalid canonical UTF-8")
		}
		return s, nil
	case 'i':
		start := p.pos
		for p.pos < len(p.data) && p.data[p.pos] != ';' {
			p.pos++
		}
		if p.pos == len(p.data) {
			return nil, fmt.Errorf("decisionview: unterminated canonical integer")
		}
		s := string(p.data[start:p.pos])
		p.pos++
		if !canonicalIntegerRe.MatchString(s) || s == "-0" {
			return nil, fmt.Errorf("decisionview: non-minimal canonical integer")
		}
		return json.Number(s), nil
	case 'a':
		n, err := p.count()
		if err != nil {
			return nil, err
		}
		a := make([]any, 0, min(n, 1024))
		for i := 0; i < n; i++ {
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		return a, nil
	case 'o':
		n, err := p.count()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, min(n, 1024))
		previous := ""
		for i := 0; i < n; i++ {
			k, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			key, ok := k.(string)
			if !ok || (i > 0 && key <= previous) {
				return nil, fmt.Errorf("decisionview: canonical object keys must be unique sorted strings")
			}
			previous = key
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		return m, nil
	default:
		return nil, fmt.Errorf("decisionview: unknown canonical tag %q", tag)
	}
}
