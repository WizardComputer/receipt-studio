package i18n

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	resources "receiptstudio.local/agent"
)

var englishJSON, _ = resources.Assets.ReadFile("locales/en.json")

var english = func() map[string]string {
	var messages map[string]string
	if err := json.Unmarshal(englishJSON, &messages); err != nil {
		panic(err)
	}
	return messages
}()

var formatToken = regexp.MustCompile(`%[0-9.]*[sdvgw]`)

type translationPattern struct {
	expression  *regexp.Regexp
	english     string
	nestedError bool
}

var translationPatterns = func() []translationPattern {
	keys := []string{}
	for key := range english {
		if formatToken.MatchString(key) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})
	patterns := []translationPattern{}
	for _, key := range keys {
		parts := formatToken.Split(key, -1)
		for i := range parts {
			parts[i] = regexp.QuoteMeta(parts[i])
		}
		patterns = append(patterns, translationPattern{regexp.MustCompile(`(?s)^` + strings.Join(parts, `(.+?)`) + `$`), english[key], strings.HasSuffix(key, ": %w") || strings.HasSuffix(key, ": %s")})
	}
	return patterns
}()

// Only interface text goes through this function, never receipt JSON or user names.
func Translate(language, text string) string {
	if language != "en" {
		return text
	}
	if translated, ok := english[text]; ok {
		return translated
	}
	if strings.HasPrefix(text, "Error: ") {
		message := strings.TrimPrefix(text, "Error: ")
		body, trace, hasTrace := strings.Cut(message, " at ")
		result := "Error: " + Translate(language, body)
		if hasTrace {
			result += " at " + trace
		}
		return result
	}
	for _, pattern := range translationPatterns {
		if match := pattern.expression.FindStringSubmatch(text); match != nil {
			i := 0
			return formatToken.ReplaceAllStringFunc(pattern.english, func(string) string {
				i++
				if i < len(match) {
					if pattern.nestedError && i == len(match)-1 {
						return Translate(language, match[i])
					}
					return match[i]
				}
				return ""
			})
		}
	}
	return text
}

// Old job snapshots retain their original messages; normalize their display too.
var printTerms = strings.NewReplacer("Fizička štampa nije potvrđena", "Fizičko printanje nije potvrđeno", "fizička štampa nije potvrđena", "fizičko printanje nije potvrđeno", "ponovne štampe", "ponovnog printanja", "nove štampe", "novog printanja", "novu štampu", "novo printanje", "Ponovna štampa", "Ponovno printanje", "odštampati", "isprintati", "Odštampati", "Isprintati", "štampanje", "printanje", "štampe", "printanja", "štampu", "printanje")

func BosnianPrintTerms(text string) string { return printTerms.Replace(text) }
