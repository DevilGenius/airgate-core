// Package htmlsanitize 提供可复用的富文本 HTML 安全清洗。
package htmlsanitize

import (
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	dangerousRichHTMLTags = map[string]struct{}{
		"applet": {}, "base": {}, "button": {}, "embed": {}, "form": {}, "iframe": {},
		"input": {}, "link": {}, "math": {}, "meta": {}, "object": {}, "option": {},
		"script": {}, "select": {}, "svg": {}, "textarea": {},
	}
	richHTMLURLAttributes = map[string]struct{}{
		"action": {}, "background": {}, "cite": {}, "formaction": {}, "href": {},
		"poster": {}, "src": {}, "xlink:href": {},
	}
	dangerousCSSPattern = regexp.MustCompile(`(?i)expression\s*\(|javascript\s*:|vbscript\s*:|@import|behavior\s*:|-moz-binding`)
	cssURLPattern       = regexp.MustCompile(`(?i)url\s*\(\s*([^)]*?)\s*\)`)
	cssCommentPattern   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssEscapePattern    = regexp.MustCompile(`(?i)\\([0-9a-f]{1,6})\s?|\\(.)`)
)

// Sanitize 保留常规富文本结构与安全样式，并移除可执行内容和危险 URL。
func Sanitize(body string) string {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(body), context)
	if err != nil {
		return html.EscapeString(body)
	}
	for _, node := range nodes {
		context.AppendChild(node)
	}

	var result strings.Builder
	for node := context.FirstChild; node != nil; {
		next := node.NextSibling
		sanitizeHTMLNode(node)
		if node.Parent == context {
			_ = html.Render(&result, node)
		}
		node = next
	}
	return result.String()
}

func sanitizeHTMLNode(node *html.Node) {
	if node.Type == html.ElementNode {
		tag := strings.ToLower(node.Data)
		if _, dangerous := dangerousRichHTMLTags[tag]; dangerous {
			removeHTMLNode(node)
			return
		}
		if tag == "style" {
			if hasDangerousCSS(nodeText(node)) {
				removeHTMLNode(node)
				return
			}
		}
		node.Attr = sanitizeHTMLAttributes(node.Attr)
	}

	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		sanitizeHTMLNode(child)
		child = next
	}
}

func sanitizeHTMLAttributes(attributes []html.Attribute) []html.Attribute {
	cleaned := attributes[:0]
	for _, attribute := range attributes {
		name := strings.ToLower(attribute.Key)
		value := attribute.Val
		if strings.HasPrefix(name, "on") || name == "srcdoc" {
			continue
		}
		if name == "style" {
			value = sanitizeCSS(value)
			if value == "" {
				continue
			}
			attribute.Val = value
		}
		if _, isURL := richHTMLURLAttributes[name]; isURL && !isSafeRichHTMLURL(value) {
			continue
		}
		if name == "srcset" && hasDangerousCSS(value) {
			continue
		}
		cleaned = append(cleaned, attribute)
	}
	return cleaned
}

func isSafeRichHTMLURL(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/") ||
		strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") {
		return true
	}

	lower := strings.ToLower(stripCSSControlCharacters(trimmed))
	for _, prefix := range []string{"data:image/png;", "data:image/gif;", "data:image/jpeg;", "data:image/jpg;", "data:image/webp;", "data:image/bmp;"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if parsed.Scheme == "" {
		return true
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto", "tel", "cid":
		return true
	default:
		return false
	}
}

func stripCSSControlCharacters(value string) string {
	return strings.Map(func(character rune) rune {
		if character <= 0x1f || character == 0x7f {
			return -1
		}
		return character
	}, value)
}

func sanitizeCSS(value string) string {
	declarations := splitCSSDeclarations(value)
	cleaned := declarations[:0]
	for _, declaration := range declarations {
		cleanDeclaration := stripCSSControlCharacters(strings.TrimSpace(declaration))
		normalized := normalizeCSSForSafety(cleanDeclaration)
		if normalized == "" || dangerousCSSPattern.MatchString(normalized) || hasUnsafeCSSURL(normalized) {
			continue
		}
		cleaned = append(cleaned, cleanDeclaration)
	}
	return strings.Join(cleaned, ";")
}

func hasDangerousCSS(value string) bool {
	normalized := normalizeCSSForSafety(value)
	return dangerousCSSPattern.MatchString(normalized) || hasUnsafeCSSURL(normalized)
}

func splitCSSDeclarations(value string) []string {
	var declarations []string
	start := 0
	depth := 0
	var quote rune
	escaped := false
	for index, character := range value {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				declarations = append(declarations, value[start:index])
				start = index + 1
			}
		}
	}
	declarations = append(declarations, value[start:])
	return declarations
}

func hasUnsafeCSSURL(value string) bool {
	for _, match := range cssURLPattern.FindAllStringSubmatch(value, -1) {
		candidate := strings.Trim(strings.TrimSpace(match[1]), `"'`)
		if !isSafeRichHTMLURL(candidate) {
			return true
		}
	}
	return false
}

func normalizeCSSForSafety(value string) string {
	withoutComments := cssCommentPattern.ReplaceAllString(value, "")
	decoded := cssEscapePattern.ReplaceAllStringFunc(withoutComments, func(match string) string {
		parts := cssEscapePattern.FindStringSubmatch(match)
		if parts[1] == "" {
			return parts[2]
		}
		var character rune
		for _, digit := range strings.ToLower(parts[1]) {
			character *= 16
			switch {
			case digit >= '0' && digit <= '9':
				character += digit - '0'
			case digit >= 'a' && digit <= 'f':
				character += digit - 'a' + 10
			}
		}
		return string(character)
	})
	return stripCSSControlCharacters(decoded)
}

func nodeText(node *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}

func removeHTMLNode(node *html.Node) {
	if node.Parent != nil {
		node.Parent.RemoveChild(node)
	}
}
