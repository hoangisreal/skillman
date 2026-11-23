package tui

import "fmt"

type language string

const (
	english    language = "en"
	vietnamese language = "vi"
)

func (l language) text(message string) string {
	if l == vietnamese {
		if translation, ok := vietnameseMessages[message]; ok {
			return translation
		}
	}
	return message
}

func (l language) format(message string, args ...any) string {
	return fmt.Sprintf(l.text(message), args...)
}
