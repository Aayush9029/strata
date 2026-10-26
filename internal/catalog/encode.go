package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// MarshalXcode encodes value like Python's json.dumps(value, indent=2,
// ensure_ascii=False, sort_keys=True, separators=(",", " : ")) plus a
// trailing newline, which is the format string catalogs are committed in.
func MarshalXcode(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := writeJSONValue(&buffer, value, 0); err != nil {
		return nil, err
	}
	buffer.WriteByte('\n')
	return buffer.Bytes(), nil
}

func writeJSONValue(buffer *bytes.Buffer, value any, depth int) error {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			buffer.WriteString("{}")
			return nil
		}
		buffer.WriteString("{\n")
		for index, key := range sortedKeys(typed) {
			if index > 0 {
				buffer.WriteString(",\n")
			}
			writeIndent(buffer, depth+1)
			writeJSONString(buffer, key)
			buffer.WriteString(" : ")
			if err := writeJSONValue(buffer, typed[key], depth+1); err != nil {
				return err
			}
		}
		buffer.WriteByte('\n')
		writeIndent(buffer, depth)
		buffer.WriteByte('}')
	case []any:
		if len(typed) == 0 {
			buffer.WriteString("[]")
			return nil
		}
		buffer.WriteString("[\n")
		for index, element := range typed {
			if index > 0 {
				buffer.WriteString(",\n")
			}
			writeIndent(buffer, depth+1)
			if err := writeJSONValue(buffer, element, depth+1); err != nil {
				return err
			}
		}
		buffer.WriteByte('\n')
		writeIndent(buffer, depth)
		buffer.WriteByte(']')
	case string:
		writeJSONString(buffer, typed)
	case json.Number:
		buffer.WriteString(typed.String())
	case bool:
		buffer.WriteString(strconv.FormatBool(typed))
	case nil:
		buffer.WriteString("null")
	case int:
		buffer.WriteString(strconv.Itoa(typed))
	case float64:
		buffer.WriteString(strconv.FormatFloat(typed, 'g', -1, 64))
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func writeIndent(buffer *bytes.Buffer, depth int) {
	for range depth {
		buffer.WriteString("  ")
	}
}

func writeJSONString(buffer *bytes.Buffer, text string) {
	buffer.WriteByte('"')
	for _, char := range text {
		switch char {
		case '"':
			buffer.WriteString(`\"`)
		case '\\':
			buffer.WriteString(`\\`)
		case '\n':
			buffer.WriteString(`\n`)
		case '\r':
			buffer.WriteString(`\r`)
		case '\t':
			buffer.WriteString(`\t`)
		case '\b':
			buffer.WriteString(`\b`)
		case '\f':
			buffer.WriteString(`\f`)
		default:
			if char < 0x20 {
				fmt.Fprintf(buffer, `\u%04x`, char)
			} else {
				buffer.WriteRune(char)
			}
		}
	}
	buffer.WriteByte('"')
}
