package bcl

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func LoadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%s:%d: empty env key", path, lineNo)
		}
		val = strings.TrimSpace(stripEnvComment(val))
		decoded, err := decodeEnvValue(val)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		values[key] = decoded
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func LoadEnvFiles(paths ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		values, err := LoadEnvFile(path)
		if err != nil {
			return nil, err
		}
		for k, v := range values {
			out[k] = v
		}
	}
	return out, nil
}

func stripEnvComment(s string) string {
	var quote rune
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' || r == '`' {
			quote = r
			continue
		}
		if r == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

func decodeEnvValue(s string) (string, error) {
	if len(s) < 2 {
		return s, nil
	}
	quote := s[0]
	if quote != '"' && quote != '\'' && quote != '`' {
		return s, nil
	}
	if s[len(s)-1] != quote {
		return "", fmt.Errorf("unterminated quoted env value")
	}
	body := s[1 : len(s)-1]
	if quote == '\'' || quote == '`' {
		return body, nil
	}
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' || i+1 >= len(body) {
			b.WriteByte(body[i])
			continue
		}
		i++
		switch body[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '\\', '"':
			b.WriteByte(body[i])
		default:
			b.WriteByte(body[i])
		}
	}
	return b.String(), nil
}
