package textsearch

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type compiledPattern struct {
	id         string
	expression *regexp.Regexp
	wholeWord  bool
}

func compilePattern(pattern Pattern) (compiledPattern, error) {
	if pattern.Query == "" {
		return compiledPattern{}, fmt.Errorf("%w: query is required", ErrInvalidRequest)
	}
	expression := pattern.Query
	if !pattern.Regex {
		expression = regexp.QuoteMeta(expression)
	}
	if !pattern.CaseSensitive {
		expression = "(?i)" + expression
	}
	compiled, err := regexp.Compile(expression)
	if err != nil {
		return compiledPattern{}, fmt.Errorf("%w: invalid regular expression: %v", ErrInvalidRequest, err)
	}
	if compiled.MatchString("") {
		return compiledPattern{}, fmt.Errorf("%w: patterns matching empty text are not supported", ErrInvalidRequest)
	}
	return compiledPattern{id: pattern.ID, expression: compiled, wholeWord: pattern.WholeWord}, nil
}

func (p compiledPattern) find(line string) [][2]int {
	locations := p.expression.FindAllStringIndex(line, -1)
	result := make([][2]int, 0, len(locations))
	for _, location := range locations {
		if p.wholeWord && !wordBoundary(line, location[0], location[1]) {
			continue
		}
		result = append(result, [2]int{location[0], location[1]})
	}
	return result
}

func wordBoundary(value string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(value[:start])
		if wordRune(r) {
			return false
		}
	}
	if end < len(value) {
		r, _ := utf8.DecodeRuneInString(value[end:])
		if wordRune(r) {
			return false
		}
	}
	return true
}

func wordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) }

func normalizeExtension(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "" && !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	return value
}
