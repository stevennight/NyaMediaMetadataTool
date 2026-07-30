package mediapath

import (
	"path"
	"strings"
)

// Base returns the final path element for either Windows or POSIX paths.
func Base(value string) string {
	cleaned := Clean(value)
	if cleaned == "." {
		return "."
	}
	index := strings.LastIndexAny(cleaned, `/\`)
	if index < 0 {
		return cleaned
	}
	if index == len(cleaned)-1 {
		return cleaned[index:]
	}
	return cleaned[index+1:]
}

// Dir returns all but the final path element while preserving the input style.
func Dir(value string) string {
	cleaned := Clean(value)
	index := strings.LastIndexAny(cleaned, `/\`)
	if index < 0 {
		return "."
	}
	if index == 0 {
		return cleaned[:1]
	}
	if index == 2 && isWindowsDrive(cleaned) {
		return cleaned[:3]
	}
	return cleaned[:index]
}

// Ext returns the suffix beginning at the final dot in the final path element.
func Ext(value string) string {
	base := Base(value)
	index := strings.LastIndexByte(base, '.')
	if index < 0 {
		return ""
	}
	return base[index:]
}

// Clean normalizes dot segments while preserving Windows or POSIX separators.
func Clean(value string) string {
	if value == "" {
		return "."
	}
	separator := separatorFor(value)
	normalized := strings.ReplaceAll(value, `\`, "/")
	unc := strings.HasPrefix(normalized, "//")
	cleaned := path.Clean(normalized)
	if unc && !strings.HasPrefix(cleaned, "//") {
		cleaned = "/" + cleaned
	}
	if separator == '\\' {
		cleaned = strings.ReplaceAll(cleaned, "/", `\`)
	}
	return cleaned
}

// Join appends a relative element while preserving the base path style.
func Join(base string, element string) string {
	if base == "" {
		return Clean(element)
	}
	if element == "" {
		return Clean(base)
	}
	separator := separatorFor(base)
	joined := strings.TrimRight(base, `/\`) + string(separator) + strings.TrimLeft(element, `/\`)
	return Clean(joined)
}

func separatorFor(value string) byte {
	if strings.Contains(value, `\`) {
		return '\\'
	}
	return '/'
}

func isWindowsDrive(value string) bool {
	return len(value) >= 2 &&
		((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':'
}
