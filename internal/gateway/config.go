package gateway

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"handoffguard/internal/strictjson"
)

type Mapping struct {
	Action string `json:"action"`
	// Each category maps to a JSON pointer into tool arguments. The selected
	// value must be a concrete string ID or a nonempty array of string IDs.
	Resources   map[string]string `json:"resources"`
	DataClasses []string          `json:"data_classes"`
}
type Config struct {
	Tools map[string]Mapping `json:"tools"`
}

func LoadConfig(path string) (Config, error) {
	var config Config
	f, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return config, err
	}
	if len(raw) > 65536 {
		return config, fmt.Errorf("gateway configuration exceeds 64 KiB")
	}
	if err := strictjson.Decode(raw, &config); err != nil {
		return config, fmt.Errorf("invalid gateway config: %w", err)
	}
	return config, config.Validate()
}
func (c Config) Validate() error {
	if len(c.Tools) == 0 || len(c.Tools) > 256 {
		return fmt.Errorf("configure between 1 and 256 tools")
	}
	for name, m := range c.Tools {
		if !identifier(name) || !identifier(m.Action) {
			return fmt.Errorf("tool names and actions must be exact nonblank identifiers")
		}
		if len(m.Resources) == 0 {
			return fmt.Errorf("tool %s needs a resource mapping", name)
		}
		for category, pointer := range m.Resources {
			if !identifier(category) || strings.Contains(category, ":") {
				return fmt.Errorf("invalid resource category for %s", name)
			}
			if _, err := pointerTokens(pointer); err != nil {
				return fmt.Errorf("tool %s: %w", name, err)
			}
		}
		for _, class := range m.DataClasses {
			if !identifier(class) {
				return fmt.Errorf("invalid data class for %s", name)
			}
		}
	}
	return nil
}
func identifier(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "*?[]\r\n\x00")
}
func pointerTokens(pointer string) ([]string, error) {
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("resource argument must be a JSON pointer beginning with /")
	}
	tokens := strings.Split(pointer[1:], "/")
	for i, token := range tokens {
		var out strings.Builder
		for j := 0; j < len(token); j++ {
			if token[j] != '~' {
				out.WriteByte(token[j])
				continue
			}
			j++
			if j >= len(token) {
				return nil, fmt.Errorf("invalid JSON pointer escape")
			}
			switch token[j] {
			case '0':
				out.WriteByte('~')
			case '1':
				out.WriteByte('/')
			default:
				return nil, fmt.Errorf("invalid JSON pointer escape")
			}
		}
		tokens[i] = out.String()
	}
	return tokens, nil
}
func extract(arguments map[string]any, mapping Mapping) (map[string][]string, error) {
	result := map[string][]string{}
	for category, pointer := range mapping.Resources {
		tokens, err := pointerTokens(pointer)
		if err != nil {
			return nil, err
		}
		var value any = arguments
		for _, token := range tokens {
			switch current := value.(type) {
			case map[string]any:
				value = current[token]
			case []any:
				index, err := strconv.Atoi(token)
				if err != nil || index < 0 || index >= len(current) || strconv.Itoa(index) != token {
					return nil, fmt.Errorf("resource argument is missing")
				}
				value = current[index]
			default:
				return nil, fmt.Errorf("resource argument is missing")
			}
		}
		var ids []string
		switch v := value.(type) {
		case string:
			ids = []string{v}
		case []any:
			for _, item := range v {
				id, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("resource IDs must be strings")
				}
				ids = append(ids, id)
			}
		default:
			return nil, fmt.Errorf("resource argument must be a string ID or array of IDs")
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("resource ID array must not be empty")
		}
		seen := map[string]bool{}
		result[category] = []string{}
		for _, id := range ids {
			if !identifier(id) || len(id) > 512 {
				return nil, fmt.Errorf("resource IDs must be concrete strings of at most 512 bytes")
			}
			if !seen[id] {
				result[category] = append(result[category], id)
				seen[id] = true
			}
		}
		sort.Strings(result[category])
	}
	return result, nil
}
