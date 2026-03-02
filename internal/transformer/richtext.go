package transformer

import (
	"strings"

	"golang.org/x/net/html"
)

func StripHTML(input string) string {
	if input == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return input
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return b.String()
}

func ADFToPlainText(node interface{}) string {
	if node == nil {
		return ""
	}
	m, ok := node.(map[string]interface{})
	if !ok {
		return ""
	}
	var b strings.Builder
	walkADF(m, &b)
	return strings.TrimSpace(b.String())
}

func walkADF(node map[string]interface{}, b *strings.Builder) {
	if t, ok := node["type"].(string); ok && t == "text" {
		if text, ok := node["text"].(string); ok {
			b.WriteString(text)
		}
		return
	}
	content, ok := node["content"].([]interface{})
	if !ok {
		return
	}
	for _, child := range content {
		if cm, ok := child.(map[string]interface{}); ok {
			walkADF(cm, b)
		}
	}
	if t, ok := node["type"].(string); ok {
		switch t {
		case "paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock":
			b.WriteByte('\n')
		}
	}
}
