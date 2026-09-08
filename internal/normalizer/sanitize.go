package normalizer

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var whitespaceRunRe = regexp.MustCompile(`\s+`)

// SanitizeDescription converts a scraped job description's HTML into
// plain text with simple markdown-style bullets, using a real HTML
// tokenizer rather than regex tag-stripping. That choice matters beyond
// robustness against malformed markup: naively deleting "<...>"
// substrings would still leave a word split by inline formatting (e.g.
// "Doc<b>ker</b>") as two separate fragments, whereas a tokenizer that
// only reacts to specific tags and otherwise just concatenates text
// naturally rejoins it into "Docker" — exactly the kind of split that
// silently defeats ExtractStack's word-boundary matching downstream.
//
// <script>/<style> content is dropped entirely, never treated as visible
// text. <li> becomes a "- " bullet; <p>/<div>/<h1-6> get blank-line
// separation; <br> is a line break. Everything else (bold, spans,
// classes, attributes, ...) is stripped down to its own text content.
func SanitizeDescription(raw string) string {
	if raw == "" {
		return ""
	}

	var b strings.Builder
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	skipDepth := 0

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return collapseWhitespace(b.String())

		case html.TextToken:
			if skipDepth == 0 {
				// Collapse each whitespace *run* (including a source
				// newline used purely for HTML formatting, e.g.
				// "<li>\n\tSome item\n</li>") down to a single space,
				// but — unlike strings.Fields — without trimming the
				// node's own leading/trailing edge: a real word-
				// separating space around an inline tag, as in
				// "with <strong>Docker</strong>", must survive, or
				// "with" and "Docker" fuse into one word. A node that's
				// nothing but whitespace (the formatting-only text
				// between sibling block tags, e.g. "</li>\n\t<li>")
				// still contributes nothing, so it can't be mistaken for
				// one of the line breaks this function injects on
				// purpose at tag boundaries. Any double space this
				// leaves where an edge-preserved node meets the "\n- "/
				// text before it is mopped up by collapseWhitespace's
				// own per-line pass below.
				text := whitespaceRunRe.ReplaceAllString(string(tokenizer.Text()), " ")
				if strings.TrimSpace(text) != "" {
					b.WriteString(text)
				}
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)

			if isRawTextElement(tag) {
				skipDepth++
				continue
			}

			switch tag {
			case "li":
				b.WriteString("\n- ")
			case "br":
				b.WriteString("\n")
			case "p", "div", "ul", "ol", "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteString("\n")
			}

		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)

			if isRawTextElement(tag) {
				if skipDepth > 0 {
					skipDepth--
				}
				continue
			}

			switch tag {
			// Not "li": its separation from the next item is handled by
			// that item's own leading "\n- ", so ending it here too
			// would insert a blank line between every bullet.
			case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteString("\n")
			}
		}
	}
}

func isRawTextElement(tag string) bool {
	return tag == "script" || tag == "style"
}

// collapseWhitespace turns the tokenizer's raw output — arbitrary
// whitespace runs from the original markup, plus the newlines
// SanitizeDescription injects at tag boundaries — into clean lines:
// internal whitespace collapsed to single spaces, at most one blank line
// between paragraphs/sections, no leading or trailing blank lines.
func collapseWhitespace(s string) string {
	s = strings.ReplaceAll(s, " ", " ") // &nbsp; decodes to this, not a plain space

	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	prevBlank := true // suppresses leading blank lines too

	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		cleaned = append(cleaned, line)
	}

	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}

	return strings.Join(cleaned, "\n")
}
