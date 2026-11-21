package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/hoangisreal/skillman/internal/core"
)

// renderMarkdown is a small, fast Markdown-to-terminal renderer for SKILL.md previews: headings, lists,
// quotes, code fences and inline `code` / **bold**. It wraps to w and never re-flows code blocks.
func renderMarkdown(text string, w int, maxBytes int) []string {
	if w < 10 {
		w = 10
	}
	if maxBytes > 0 && len(text) > maxBytes {
		text = text[:maxBytes] + "\n\n… (truncated; enter = full view)"
	}
	var out []string
	body := text
	if strings.HasPrefix(text, "---") {
		parts := strings.SplitN(text, "---", 3)
		if len(parts) == 3 {
			fm, _ := core.ParseFrontmatter(text)
			body = parts[2]
			if fm != nil {
				if d, ok := fm["description"].(string); ok {
					for _, l := range wrapPlain(strings.Join(strings.Fields(d), " "), w) {
						out = append(out, stAccent.Render(stripInline(l)))
					}
				}
				for _, k := range []string{"license", "allowed-tools", "version", "compatibility"} {
					if v, ok := fm[k]; ok {
						out = append(out, stDim.Render(ansi.Truncate(k+": "+strings.TrimSpace(fmtAny(v)), w, "…")))
					}
				}
				if _, ok := fm["hooks"]; ok {
					out = append(out, stErr.Render("hooks: (registers hooks — review!)"))
				}
				out = append(out, stBorder.Render(strings.Repeat("─", w)))
			}
		}
	}
	inCode := false
	// paragraphs and list items: consecutive source lines are joined (Markdown soft breaks), then wrapped
	var para []string
	bullet, pad := "", ""
	flush := func() {
		if len(para) == 0 {
			return
		}
		text := strings.Join(para, " ")
		para = para[:0]
		if bullet == "" {
			for _, l := range wrapPlain(text, w) {
				out = append(out, inline(l))
			}
			return
		}
		bw := len([]rune(bullet))
		for i, l := range wrapPlain(text, w-len(pad)-bw) {
			if i == 0 {
				out = append(out, pad+stAccent.Render(bullet)+inline(l))
			} else {
				out = append(out, pad+strings.Repeat(" ", bw)+inline(l))
			}
		}
		bullet, pad = "", ""
	}
	blank := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\t", "    "), "\n") {
		line := strings.TrimRight(raw, " \r")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			flush()
			inCode = !inCode
			lang := strings.TrimLeft(trim, "`~")
			if inCode && lang != "" {
				out = append(out, stDim.Render("  ┌ "+lang))
			} else {
				out = append(out, stDim.Render("  ─"))
			}
			continue
		}
		if inCode {
			out = append(out, stCode.Render(ansi.Truncate("  "+line, w, "…")))
			continue
		}
		switch {
		case trim == "":
			flush()
			blank()
		case strings.HasPrefix(trim, "#"):
			flush()
			lvl := len(trim) - len(strings.TrimLeft(trim, "#"))
			t := strings.TrimSpace(strings.TrimLeft(trim, "#"))
			blank()
			st := stH2
			if lvl == 1 {
				st = stH1
			}
			for _, l := range wrapPlain(stripInline(t), w) {
				out = append(out, st.Render(l))
			}
		case strings.HasPrefix(trim, "|"):
			flush()
			out = append(out, inline(ansi.Truncate(trim, w, "…")))
		case strings.HasPrefix(trim, ">"):
			flush()
			for _, l := range wrapPlain(strings.TrimSpace(strings.TrimPrefix(trim, ">")), w-2) {
				out = append(out, stDim.Render("│ ")+stDim.Render(l))
			}
		default:
			if b, rest := listItem(trim); b != "" {
				flush()
				indent := len(line) - len(strings.TrimLeft(line, " "))
				bullet, pad = b, strings.Repeat(" ", min(indent, 8))
				para = append(para, rest)
			} else {
				para = append(para, trim)
			}
		}
	}
	flush()
	return out
}

func fmtAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var ps []string
		for _, e := range x {
			ps = append(ps, fmtAny(e))
		}
		return strings.Join(ps, ", ")
	default:
		b, _ := core.MarshalPy(v, 0)
		return strings.TrimSpace(string(b))
	}
}

func listItem(t string) (string, string) {
	if len(t) > 2 && (t[0] == '-' || t[0] == '*' || t[0] == '+') && t[1] == ' ' {
		if strings.HasPrefix(t, "- [ ] ") {
			return "☐ ", t[6:]
		}
		if strings.HasPrefix(t, "- [x] ") || strings.HasPrefix(t, "- [X] ") {
			return "☑ ", t[6:]
		}
		return "• ", t[2:]
	}
	i := 0
	for i < len(t) && i < 3 && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' ' {
		return t[:i+1] + " ", t[i+2:]
	}
	return "", t
}

func stripInline(s string) string {
	return strings.NewReplacer("**", "", "`", "", "__", "").Replace(s)
}

// inline styles `code` and **bold** spans of an already wrapped plain line.
func inline(s string) string {
	if !strings.ContainsAny(s, "`*") {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		switch {
		case s[0] == '`':
			if j := strings.IndexByte(s[1:], '`'); j >= 0 {
				b.WriteString(stCode.Render(s[1 : j+1]))
				s = s[j+2:]
				continue
			}
		case strings.HasPrefix(s, "**"):
			if j := strings.Index(s[2:], "**"); j >= 0 {
				b.WriteString(stBold.Render(s[2 : j+2]))
				s = s[j+4:]
				continue
			}
		}
		b.WriteByte(s[0])
		s = s[1:]
	}
	return b.String()
}
