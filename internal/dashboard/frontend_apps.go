package dashboard

import (
	"regexp"
	"strings"
)

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)
var multiHyphenRe = regexp.MustCompile(`-+`)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = slugRe.ReplaceAllString(s, "")
	s = multiHyphenRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
