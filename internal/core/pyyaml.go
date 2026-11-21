package core

import (
	"regexp"
	"strings"
)

// A tiny emitter for pack files that reproduces PyYAML's
// yaml.safe_dump(doc, sort_keys=False, allow_unicode=True, width=100) output for flat mappings of
// strings and string lists, so packs edited by skillman produce no noise in the library's git history.

const yamlWidth = 100

var (
	resBool  = regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	resFloat = regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	resInt   = regexp.MustCompile(`^(?:[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x[0-9a-fA-F_]+|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	resNull  = regexp.MustCompile(`^(?:~|null|Null|NULL|)$`)
	resTime  = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)
)

func implicitNonStr(s string) bool {
	return resBool.MatchString(s) || resFloat.MatchString(s) || resInt.MatchString(s) || resNull.MatchString(s) ||
		resTime.MatchString(s) || s == "<<" || s == "="
}

func isWS(r rune) bool {
	return r == 0 || r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == 0x85 || r == 0x2028 || r == 0x2029
}

func isBreak(r rune) bool { return r == '\n' || r == 0x85 || r == 0x2028 || r == 0x2029 }

// yamlStyle -> "plain", "single" or "double" (PyYAML analyze_scalar + choose_scalar_style, block context).
func yamlStyle(s string) string {
	if s == "" || implicitNonStr(s) {
		return "single"
	}
	r := []rune(s)
	var blockInd, lineBreaks, special, leadSpace, trailSpace, leadBreak, trailBreak, breakSpace, spaceBreak bool
	if strings.HasPrefix(s, "---") || strings.HasPrefix(s, "...") {
		blockInd = true
	}
	preceded := true
	followed := len(r) == 1 || isWS(r[1])
	prevSpace, prevBreak := false, false
	for i, ch := range r {
		if i == 0 {
			if strings.ContainsRune("#,[]{}&*!|>'\"%@`", ch) {
				blockInd = true
			}
			if (ch == '?' || ch == ':') && followed {
				blockInd = true
			}
			if ch == '-' && followed {
				blockInd = true
			}
		} else {
			if ch == ':' && followed {
				blockInd = true
			}
			if ch == '#' && preceded {
				blockInd = true
			}
		}
		if isBreak(ch) {
			lineBreaks = true
		}
		if !(ch == '\n' || (ch >= 0x20 && ch <= 0x7e)) {
			uni := (ch == 0x85 || (ch >= 0xa0 && ch <= 0xd7ff) || (ch >= 0xe000 && ch <= 0xfffd) ||
				(ch >= 0x10000 && ch < 0x10ffff)) && ch != 0xfeff
			if !uni {
				special = true
			}
		}
		switch {
		case ch == ' ':
			if i == 0 {
				leadSpace = true
			}
			if i == len(r)-1 {
				trailSpace = true
			}
			if prevBreak {
				breakSpace = true
			}
			prevSpace, prevBreak = true, false
		case isBreak(ch):
			if i == 0 {
				leadBreak = true
			}
			if i == len(r)-1 {
				trailBreak = true
			}
			if prevSpace {
				spaceBreak = true
			}
			prevSpace, prevBreak = false, true
		default:
			prevSpace, prevBreak = false, false
		}
		preceded = isWS(ch)
		followed = i+2 >= len(r) || isWS(r[i+2])
	}
	plain, single := true, true
	if leadSpace || leadBreak || trailSpace || trailBreak {
		plain = false
	}
	if breakSpace {
		plain, single = false, false
	}
	if spaceBreak || special {
		plain, single = false, false
	}
	if lineBreaks || blockInd {
		plain = false
	}
	switch {
	case plain:
		return "plain"
	case single:
		return "single"
	}
	return "double"
}

// yamlScalar renders s as the value of a mapping key or sequence item whose text so far ends at column col.
// The returned string starts with the separating space.
func yamlScalar(s string, col int) string {
	var b strings.Builder
	r := []rune(s)
	switch yamlStyle(s) {
	case "plain":
		b.WriteByte(' ')
		col++
		spaces := false
		start := 0
		for end := 0; end <= len(r); end++ {
			var ch rune = -1
			if end < len(r) {
				ch = r[end]
			}
			if spaces {
				if ch != ' ' {
					if start+1 == end && col > yamlWidth {
						b.WriteString("\n  ")
						col = 2
					} else {
						b.WriteString(string(r[start:end]))
						col += end - start
					}
					start = end
				}
			} else if ch == -1 || ch == ' ' {
				b.WriteString(string(r[start:end]))
				col += end - start
				start = end
			}
			spaces = ch == ' '
		}
	case "single":
		b.WriteString(" '")
		col += 2
		spaces, breaks := false, false
		start := 0
		for end := 0; end <= len(r); end++ {
			var ch rune = -1
			if end < len(r) {
				ch = r[end]
			}
			if breaks {
				if ch == -1 || !isBreak(ch) {
					if r[start] == '\n' {
						b.WriteByte('\n')
					}
					for _, br := range r[start:end] {
						if br == '\n' {
							b.WriteByte('\n')
						} else {
							b.WriteRune(br)
						}
					}
					b.WriteString("  ")
					col = 2
					start = end
				}
			} else if spaces {
				if ch != ' ' {
					if start+1 == end && col > yamlWidth && start != 0 && end != len(r) {
						b.WriteString("\n  ")
						col = 2
					} else {
						b.WriteString(string(r[start:end]))
						col += end - start
					}
					start = end
				}
			} else if ch == -1 || ch == ' ' || isBreak(ch) || ch == '\'' {
				if start < end {
					b.WriteString(string(r[start:end]))
					col += end - start
					start = end
				}
			}
			if ch == '\'' {
				b.WriteString("''")
				col += 2
				start = end + 1
			}
			spaces, breaks = ch == ' ', ch != -1 && isBreak(ch)
		}
		b.WriteByte('\'')
	default:
		b.WriteByte(' ')
		b.WriteString(jsonQuote(s))
	}
	return b.String()
}

func jsonQuote(s string) string {
	bs, _ := MarshalPy(s, 0)
	return strings.TrimSuffix(string(bs), "\n")
}

type yamlField struct {
	key  string
	str  *string
	list []string
}

// dumpYAML emits an ordered flat mapping of strings / string lists.
func dumpYAML(fields []yamlField) string {
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(f.key + ":")
		if f.str != nil {
			b.WriteString(yamlScalar(*f.str, len([]rune(f.key))+1))
			b.WriteByte('\n')
			continue
		}
		if len(f.list) == 0 {
			b.WriteString(" []\n")
			continue
		}
		b.WriteByte('\n')
		for _, it := range f.list {
			b.WriteString("-" + yamlScalar(it, 1) + "\n")
		}
	}
	return b.String()
}
