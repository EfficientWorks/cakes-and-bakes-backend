package report

import "fmt"

func hourLabel12h(h int) string {
	suffix := "AM"
	label := h
	if h == 0 {
		label = 12
	} else if h == 12 {
		suffix = "PM"
	} else if h > 12 {
		label = h - 12
		suffix = "PM"
	}
	return fmt.Sprintf("%d%s", label, suffix)
}
