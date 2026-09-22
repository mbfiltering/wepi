package wepi

import (
	"log"
	"regexp"
	"strings"
)

var matcher = "([^/]+)"

// PathReader holds a compiled regex pattern for matching URL path templates.
type PathReader struct {
	keys    []string
	pattern string
	regex   *regexp.Regexp
}

func (w *WepiController) addPattern(path string) {
	w.pathsMutex.Lock()
	defer w.pathsMutex.Unlock()
	reg, keys := buildRegexFromTemplate(path)
	if len(keys) > 0 {
		w.addRegex(reg, keys, path)
	}
}

func (w *WepiController) addRegex(regex *regexp.Regexp, keys []string, pattern string) {
	w.pathRegexs = append(w.pathRegexs, &PathReader{regex: regex, keys: keys, pattern: pattern})
}

func (w *WepiController) checkPatternsForPath(path string) (map[string]string, string) {
	for _, pReader := range w.pathRegexs {
		m := extractPatternValues(pReader.regex, pReader.keys, path)
		if m != nil {
			return m, pReader.pattern
		}
	}
	return nil, ""
}

// loadRouteFromRequest finds a registered route for the given path and method.
func (w *WepiController) loadRouteFromRequest(path string, method string) (newPath string, _ *Route, pathPatternParams map[string]string) {
	// A literal route wins over a pattern that would otherwise capture one of its segments as a param
	if r, ok := w.routes.Load(path + method); ok {
		return path, r.(*Route), nil
	}
	pathPatternParams, foundPatternPath := w.checkPatternsForPath(path)

	if foundPatternPath != "" {
		path = foundPatternPath
	} else {
		pathPatternParams = nil
	}

	r, ok := w.routes.Load(path + method)
	if !ok {
		return "", nil, nil
	}

	return path, r.(*Route), pathPatternParams
}

// buildRegexFromTemplate converts a path template like "/users/{id}/posts/{postId}"
// into a compiled regex and returns the ordered list of parameter names.
func buildRegexFromTemplate(template string) (*regexp.Regexp, []string) {
	re := regexp.MustCompile(`\{([^{}]+)\}`)
	var keys []string

	pattern := re.ReplaceAllStringFunc(template, func(s string) string {
		m := re.FindStringSubmatch(s)
		keys = append(keys, m[1])
		return matcher
	})

	pattern = "^" + pattern + "$"

	if len(keys) == 0 {
		return nil, nil
	}

	// Reject ambiguous consecutive captures like {a}{b}
	if strings.Contains(pattern, matcher+matcher) {
		log.Println("not valid pattern: " + pattern + " for path: " + template)
		return nil, nil
	}

	compiledRe, err := regexp.Compile(pattern)
	if err != nil {
		log.Println(err)
		return nil, nil
	}
	return compiledRe, keys
}

// extractPatternValues matches a path against a compiled regex and returns
// parameter names mapped to their captured values. Returns nil if no match.
func extractPatternValues(re *regexp.Regexp, keys []string, path string) map[string]string {
	match := re.FindStringSubmatch(path)
	if match == nil {
		return nil // No match found
	}
	values := make(map[string]string)
	for i, key := range keys {
		values[key] = match[i+1] // match[0] is the full match
	}
	return values
}
