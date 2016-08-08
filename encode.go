
package hjson

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type EncoderOptions struct {
	Eol string
	BracesSameLine bool
	EmitRootBraces bool
	QuoteAlways bool
	IndentBy string
	AllowMinusZero bool
	UnknownAsNull bool
}

func DefaultOptions() EncoderOptions {
    opt := EncoderOptions{}
	opt.Eol = "\n"
	opt.BracesSameLine = false
	opt.EmitRootBraces = true
	opt.QuoteAlways = false
	opt.IndentBy = "  "
	opt.AllowMinusZero = false
	opt.UnknownAsNull = false
    return opt
}

type hjsonEncoder struct {
	bytes.Buffer // output
	EncoderOptions
	indent int
}

var needsEscape, needsQuotes, needsQuotes2, needsEscapeML, startsWithKeyword, needsEscapeName *regexp.Regexp

func init() {
	var err error
	// needsEscape is used to detect and replace characters
	needsEscape, err = regexp.Compile(`[\\\"\x00-\x1f\x7f-\x9f\x{00ad}\x{0600}-\x{0604}\x{070f}\x{17b4}\x{17b5}\x{200c}-\x{200f}\x{2028}-\x{202f}\x{2060}-\x{206f}\x{feff}\x{fff0}-\x{ffff}]`)
	if err != nil { panic(err) }
	// like needsEscape but without \\ and \"
	needsQuotes, err = regexp.Compile(`[\x00-\x1f\x7f-\x9f\x{00ad}\x{0600}-\x{0604}\x{070f}\x{17b4}\x{17b5}\x{200c}-\x{200f}\x{2028}-\x{202f}\x{2060}-\x{206f}\x{feff}\x{fff0}-\x{ffff}]`)
	if err != nil { panic(err) }
	needsQuotes2, err = regexp.Compile(`^\s|^"|^'''|^#|^/\*|^//|^\{|^\[|\s$`)
	if err != nil { panic(err) }
	// ''' || (needsQuotes but without \n and \r)
	needsEscapeML, err = regexp.Compile(`'''|[\x00-\x09\x0b\x0c\x0e-\x1f\x7f-\x9f\x{00ad}\x{0600}-\x{0604}\x{070f}\x{17b4}\x{17b5}\x{200c}-\x{200f}\x{2028}-\x{202f}\x{2060}-\x{206f}\x{feff}\x{fff0}-\x{ffff}]`)
	if err != nil { panic(err) }
	// starts with a keyword and optionally is followed by a comment
	startsWithKeyword, err = regexp.Compile(`^(true|false|null)\s*((,|\]|\}|#|//|/\*).*)?$`)
	if err != nil { panic(err) }
	needsEscapeName, err = regexp.Compile(`[,\{\[\}\]\s:#"]|//|/\*|'''`)
	if err != nil { panic(err) }
}

var meta map[byte][]byte = map[byte][]byte {
	// table of character substitutions
	'\b': []byte("\\b"),
	'\t': []byte("\\t"),
	'\n': []byte("\\n"),
	'\f': []byte("\\f"),
	'\r': []byte("\\r"),
	'"' : []byte("\\\""),
	'\\': []byte("\\\\"),
}

func (e *hjsonEncoder) quoteReplace(text string) (string) {
	return string(needsEscape.ReplaceAllFunc([]byte(text), func(a []byte) []byte {
		c := meta[a[0]]
		if c != nil {
			return c
		} else {
			return []byte(fmt.Sprintf("\\u%04x", c))
		}
	}))
}

func (e *hjsonEncoder) quote(value string, separator string, isRootObject bool) {

	// Check if we can insert this string without quotes
	// see hjson syntax (must not parse as true, false, null or number)

	if len(value) == 0 {
		e.WriteString(separator + `""`)
	} else if (e.QuoteAlways ||
		needsQuotes.MatchString(value) || needsQuotes2.MatchString(value) ||
		startsWithNumber([]byte(value)) ||
		startsWithKeyword.MatchString(value)) {

		// If the string contains no control characters, no quote characters, and no
		// backslash characters, then we can safely slap some quotes around it.
		// Otherwise we first check if the string can be expressed in multiline
		// format or we must replace the offending characters with safe escape
		// sequences.

		if !needsEscape.MatchString(value) {
			e.WriteString(separator + `"` + value + `"`)
		} else if !needsEscapeML.MatchString(value) && !isRootObject {
			e.mlString(value, separator)
		} else {
			e.WriteString(separator + `"` + e.quoteReplace(value) + `"`)
		}
	} else {
		// return without quotes
		e.WriteString(separator + value)
	}
}

func (e *hjsonEncoder) mlString(value string, separator string) {
	// wrap the string into the ''' (multiline) format

	a := strings.Split(strings.Replace(value, "\r", "", -1), "\n")

	if len(a) == 1 {
		// The string contains only a single line. We still use the multiline
		// format as it avoids escaping the \ character (e.g. when used in a
		// regex).
		e.WriteString(separator + "'''")
		e.WriteString(a[0])
	} else {
		e.writeIndent(e.indent + 1)
		e.WriteString("'''")
		for _, v := range a {
			indent := e.indent + 1
			if len(v) == 0 { indent = 0 }
			e.writeIndent(indent)
			e.WriteString(v)
		}
		e.writeIndent(e.indent + 1)
	}
	e.WriteString("'''")
}

func (e *hjsonEncoder) quoteName(name string) (string) {
	if len(name) == 0 {
		return `""`
	}

	// Check if we can insert this name without quotes

	if needsEscapeName.MatchString(name) {
		if needsEscape.MatchString(name) { name = e.quoteReplace(name) }
		return `"` + name + `"`
	} else {
		// without quotes
		return name
	}
}

type SortAlpha []reflect.Value

func (s SortAlpha) Len() int {
    return len(s)
}
func (s SortAlpha) Swap(i, j int) {
    s[i], s[j] = s[j], s[i]
}
func (s SortAlpha) Less(i, j int) bool {
    return s[i].String() < s[j].String()
}

func (e *hjsonEncoder) writeIndent(indent int) {
	e.WriteString(e.Eol)
	for i := 0; i < indent; i++ {
		e.WriteString(e.IndentBy)
	}
}

func (e *hjsonEncoder) str(value reflect.Value, noIndent bool, separator string, isRootObject bool) (error) {

	// Produce a string from value.

	kind := value.Kind()

	if kind == reflect.Interface || kind == reflect.Ptr {
		if value.IsNil() {
			e.WriteString(separator)
			e.WriteString("null")
			return nil
		} else {
			value = value.Elem()
			kind = value.Kind()
		}
	}


	switch kind {
		case reflect.String:
			e.quote(value.String(), separator, isRootObject)

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Uintptr:
			e.WriteString(separator)
			e.WriteString(strconv.FormatInt(value.Int(), 10))

		case reflect.Float32, reflect.Float64:
			// JSON numbers must be finite. Encode non-finite numbers as null.
			e.WriteString(separator)
			number := value.Float()
			if math.IsInf(number, 0) || math.IsNaN(number) {
				e.WriteString("null");
			} else if !e.AllowMinusZero && number == -0 {
				e.WriteString("0")
			} else {
				// find shortest representation ('G' does not work)
				val := strconv.FormatFloat(number, 'f', -1, 64)
				exp := strconv.FormatFloat(number, 'E', -1, 64)
				if len(exp) < len(val) { val = strings.ToLower(exp) }
				e.WriteString(val)
			}

		case reflect.Bool:
			e.WriteString(separator)
			if value.Bool() {
				e.WriteString("true")
			} else {
				e.WriteString("false")
			}

		case reflect.Slice, reflect.Array:

			len := value.Len()
			if len == 0 {
				e.WriteString(separator)
				e.WriteString("[]")
				break
			}

			indent1 := e.indent
			e.indent++

			if !noIndent && !e.BracesSameLine {
				e.writeIndent(indent1)
			} else {
				e.WriteString(separator)
			}
			e.WriteString("[")

			// Join all of the element texts together, separated with newlines
			for i := 0; i < len; i++ {
				e.writeIndent(e.indent)
				if err := e.str(value.Index(i), true, "", false); err != nil { return err }
			}

			e.writeIndent(indent1)
			e.WriteString("]")

			e.indent = indent1

		case reflect.Map:

			len := value.Len()
			if len == 0 {
				e.WriteString(separator)
				e.WriteString("{}")
				break
			}

			showBraces := !isRootObject || e.EmitRootBraces
			indent1 := e.indent
			e.indent++

			if (showBraces) {
				if !noIndent && !e.BracesSameLine {
					e.writeIndent(indent1)
				} else {
					e.WriteString(separator)
				}
				e.WriteString("{")
			}

			keys := value.MapKeys()
		    sort.Sort(SortAlpha(keys))

			// Join all of the member texts together, separated with newlines
			for i := 0; i < len; i++ {
				e.writeIndent(e.indent)
				e.WriteString(e.quoteName(keys[i].String()))
				e.WriteString(":")
				if err := e.str(value.MapIndex(keys[i]), false, " ", false); err != nil { return err }
			}

			if (showBraces) {
				e.writeIndent(indent1)
				e.WriteString("}")
			}

			e.indent = indent1

		default:
			if e.UnknownAsNull {
				// Use null as a placeholder for non-JSON values.
				e.WriteString("null")
			} else {
				return errors.New("Unsupported type " + value.Type().String())
			}
	}
	return nil
}

func Marshal(v interface{}) ([]byte, error) {
	return MarshalWithOptions(v, DefaultOptions())
}

func MarshalWithOptions(v interface{}, options EncoderOptions) ([]byte, error) {
	e := &hjsonEncoder{}
	e.indent = 0
	e.Eol = options.Eol
	e.BracesSameLine = options.BracesSameLine
	e.EmitRootBraces = options.EmitRootBraces
	e.QuoteAlways = options.QuoteAlways
	e.IndentBy = options.IndentBy

	err := e.str(reflect.ValueOf(v), true, "", true)
	if err != nil {
		return nil, err
	}
	return e.Bytes(), nil
}