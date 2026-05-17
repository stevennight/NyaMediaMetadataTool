package episodeparse

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Result struct {
	Season  int
	Episode int
	Token   string
}

var defaultPatterns = []*regexp.Regexp{
	mustCompile(`(?i)s(\d{1,2})e(\d{1,4})\b`),
	mustCompile(`(?i)(?:^|[\s._-])(\d{1,2})x(\d{1,4})(?:$|[\s._-])`),
}

var numericEpisodePattern = mustCompile(`(?i)(?:^|[\s._-])(\d{1,4})$`)

var patternCache sync.Map

func Parse(name string, customPatterns []string) (Result, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Result{}, false
	}
	for _, pattern := range customPatterns {
		if result, ok := parseWithPattern(name, pattern); ok {
			return result, true
		}
	}
	for _, pattern := range defaultPatterns {
		if result, ok := parseWithRegexp(name, pattern); ok {
			return result, true
		}
	}
	if result, ok := parseNumericEpisode(name); ok {
		return result, true
	}
	return Result{}, false
}

func parseWithPattern(name string, pattern string) (Result, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return Result{}, false
	}
	value, ok := patternCache.Load(pattern)
	var re *regexp.Regexp
	if ok {
		re, _ = value.(*regexp.Regexp)
	}
	if re == nil {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return Result{}, false
		}
		re = compiled
		patternCache.Store(pattern, re)
	}
	return parseWithRegexp(name, re)
}

func parseWithRegexp(name string, re *regexp.Regexp) (Result, bool) {
	match := re.FindStringSubmatch(name)
	if len(match) == 0 {
		return Result{}, false
	}
	season, episode, ok := extractSeasonEpisode(re, match)
	if !ok {
		return Result{}, false
	}
	return Result{Season: season, Episode: episode, Token: match[0]}, true
}

func extractSeasonEpisode(re *regexp.Regexp, match []string) (int, int, bool) {
	names := re.SubexpNames()
	var seasonValue, episodeValue int
	seasonSet := false
	episodeSet := false
	numeric := make([]int, 0, len(match)-1)
	for i := 1; i < len(match); i++ {
		value := strings.TrimSpace(match[i])
		if value == "" {
			continue
		}
		if number, err := strconv.Atoi(value); err == nil {
			numeric = append(numeric, number)
			if i < len(names) {
				switch strings.ToLower(names[i]) {
				case "season":
					seasonValue = number
					seasonSet = true
				case "episode":
					episodeValue = number
					episodeSet = true
				}
			}
		}
	}
	if seasonSet || episodeSet {
		if !seasonSet {
			seasonValue = 1
		}
		if !episodeSet {
			if len(numeric) > 0 {
				episodeValue = numeric[len(numeric)-1]
			} else {
				return 0, 0, false
			}
		}
		return seasonValue, episodeValue, true
	}
	if len(numeric) >= 2 {
		return numeric[0], numeric[1], true
	}
	if len(numeric) == 1 {
		return 1, numeric[0], true
	}
	return 0, 0, false
}

func parseNumericEpisode(name string) (Result, bool) {
	name = trimTrailingMetadata(name)
	match := numericEpisodePattern.FindStringSubmatchIndex(name)
	if len(match) != 4 {
		return Result{}, false
	}
	start := match[2]
	if start >= 2 && name[start-1] == '.' && isASCIIDigit(name[start-2]) {
		return Result{}, false
	}
	episodeToken := name[match[2]:match[3]]
	episode, err := strconv.Atoi(episodeToken)
	if err != nil {
		return Result{}, false
	}
	if len(episodeToken) == 4 && episode >= 1900 && episode <= 2099 {
		return Result{}, false
	}
	return Result{Season: 1, Episode: episode, Token: episodeToken}, true
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func trimTrailingMetadata(name string) string {
	for {
		trimmed := strings.TrimSpace(name)
		trimmed = strings.TrimRight(trimmed, " ._-")
		if trimmed == name {
			name = trimmed
			break
		}
		name = trimmed
	}
	for {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return ""
		}
		name = trimmed
		opener, ok := trailingOpener(name)
		if !ok {
			return name
		}
		index := strings.LastIndex(name[:len(name)-len(closingForOpener(opener))], opener)
		if index < 0 {
			return name
		}
		name = strings.TrimRight(strings.TrimSpace(name[:index]), " ._-")
	}
}

func trailingOpener(value string) (string, bool) {
	switch {
	case strings.HasSuffix(value, "]"):
		return "[", true
	case strings.HasSuffix(value, ")"):
		return "(", true
	case strings.HasSuffix(value, "}"):
		return "{", true
	case strings.HasSuffix(value, "】"):
		return "【", true
	case strings.HasSuffix(value, "）"):
		return "（", true
	default:
		return "", false
	}
}

func closingForOpener(opener string) string {
	switch opener {
	case "[":
		return "]"
	case "(":
		return ")"
	case "{":
		return "}"
	case "【":
		return "】"
	case "（":
		return "）"
	default:
		return ""
	}
}

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}
