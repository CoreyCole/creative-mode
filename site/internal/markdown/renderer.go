package markdown

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	gomd "github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

const defaultCodeStyle = "github-dark"

// Renderer renders markdown to HTML with syntax-highlighted code blocks.
type Renderer struct {
	highlightStyle *chroma.Style
	htmlFormatter  *html.Formatter
	mdhtmlRenderer *mdhtml.Renderer
}

// NewRenderer creates a new markdown renderer.
func NewRenderer() (*Renderer, error) {
	highlightStyle := styles.Get(defaultCodeStyle)
	htmlFormatter := html.New(html.WithClasses(true), html.TabWidth(2))
	if htmlFormatter == nil {
		return nil, errors.New("couldn't create html formatter")
	}
	mdhtmlRenderer := buildMDHTMLRenderer(highlightStyle, htmlFormatter)
	return &Renderer{highlightStyle, htmlFormatter, mdhtmlRenderer}, nil
}

// MarkdownBytesToHTML converts markdown bytes to an HTML string.
func (r *Renderer) MarkdownBytesToHTML(md []byte) string {
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock)
	htmlBytes := gomd.ToHTML(md, p, r.mdhtmlRenderer)
	return string(htmlBytes)
}

func htmlHighlight(w io.Writer, source, lang string, style *chroma.Style, formatter *html.Formatter) error {
	if lang == "" {
		lang = ""
	}
	l := lexers.Get(lang)
	if l == nil {
		l = lexers.Analyse(source)
	}
	if l == nil {
		l = lexers.Fallback
	}
	l = chroma.Coalesce(l)
	it, err := l.Tokenise(nil, source)
	if err != nil {
		return err
	}
	return formatter.Format(w, style, it)
}

func renderCode(w io.Writer, codeBlock *ast.CodeBlock, style *chroma.Style, formatter *html.Formatter) error {
	lang := string(codeBlock.Info)
	return htmlHighlight(w, string(codeBlock.Literal), lang, style, formatter)
}

func buildMDHTMLRenderer(highlightStyle *chroma.Style, htmlFormatter *html.Formatter) *mdhtml.Renderer {
	opts := mdhtml.RendererOptions{
		Flags: mdhtml.CommonFlags | mdhtml.HrefTargetBlank,
		RenderNodeHook: func(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
			if code, ok := node.(*ast.CodeBlock); ok {
				_, _ = w.Write([]byte(`<div class="my-4 rounded-lg border border-border bg-muted [&>pre]:p-4 [&>pre]:bg-transparent [&>pre]:text-foreground">`))
				if err := renderCode(w, code, highlightStyle, htmlFormatter); err != nil {
					return ast.Terminate, false
				}
				_, _ = w.Write([]byte("</div>"))
				return ast.GoToNext, true
			}
			if link, ok := node.(*ast.Link); ok {
				if entering {
					_, _ = w.Write([]byte(`<a class="font-medium text-primary hover:text-primary/80 transition-colors underline decoration-primary/30 hover:decoration-primary/80"`))
					if len(link.Destination) > 0 {
						_, _ = fmt.Fprintf(w, ` href="%s"`, link.Destination)
					}
					_, _ = w.Write([]byte(">"))
				} else {
					_, _ = w.Write([]byte("</a>"))
				}
				return ast.GoToNext, true
			}
			if list, ok := node.(*ast.List); ok {
				listTag := "ul"
				listClass := "list-disc"
				if list.ListFlags&ast.ListTypeOrdered != 0 {
					listTag = "ol"
					listClass = "list-decimal"
				}
				if entering {
					_, _ = fmt.Fprintf(w, `<%s class="%s pl-6 my-4 space-y-2">`, listTag, listClass)
				} else {
					_, _ = fmt.Fprintf(w, "</%s>", listTag)
				}
				return ast.GoToNext, true
			}
			if _, ok := node.(*ast.ListItem); ok {
				if entering {
					_, _ = w.Write([]byte(`<li class="leading-relaxed">`))
				} else {
					_, _ = w.Write([]byte("</li>"))
				}
				return ast.GoToNext, true
			}
			if heading, ok := node.(*ast.Heading); ok && entering {
				headingClass := fmt.Sprintf("myh myh-%d", min(heading.Level, 6))
				attr := heading.Attribute
				if attr == nil {
					attr = &ast.Attribute{}
				}
				attr.Classes = append(attr.Classes, []byte(headingClass))
				heading.Attribute = attr
			}
			if _, ok := node.(*ast.Table); ok {
				if entering {
					_, _ = w.Write([]byte(`<div class="table-wrapper"><table>`))
				} else {
					_, _ = w.Write([]byte("</table></div>"))
				}
				return ast.GoToNext, true
			}
			if _, ok := node.(*ast.TableHeader); ok {
				if entering {
					_, _ = w.Write([]byte(`<thead>`))
				} else {
					_, _ = w.Write([]byte("</thead>"))
				}
				return ast.GoToNext, true
			}
			if _, ok := node.(*ast.TableBody); ok {
				if entering {
					_, _ = w.Write([]byte("<tbody>"))
				} else {
					_, _ = w.Write([]byte("</tbody>"))
				}
				return ast.GoToNext, true
			}
			if _, ok := node.(*ast.TableRow); ok {
				if entering {
					_, _ = w.Write([]byte(`<tr>`))
				} else {
					_, _ = w.Write([]byte("</tr>"))
				}
				return ast.GoToNext, true
			}
			if cell, ok := node.(*ast.TableCell); ok {
				tag := "td"
				if cell.IsHeader {
					tag = "th"
				}
				if entering {
					align := ""
					switch cell.Align {
					case ast.TableAlignmentLeft:
						align = " text-left"
					case ast.TableAlignmentRight:
						align = " text-right"
					case ast.TableAlignmentCenter:
						align = " text-center"
					}
					_, _ = fmt.Fprintf(w, `<%s class="px-4 py-3 text-sm text-foreground%s">`, tag, align)
				} else {
					_, _ = fmt.Fprintf(w, "</%s>", tag)
				}
				return ast.GoToNext, true
			}
			// Handle checkbox stripping in text nodes
			if text, ok := node.(*ast.Text); ok && entering {
				content := string(text.Literal)
				if strings.HasPrefix(content, "[ ] ") || strings.HasPrefix(content, "[x] ") || strings.HasPrefix(content, "[X] ") {
					_, _ = w.Write([]byte(content[4:]))
					return ast.GoToNext, true
				}
			}
			return ast.GoToNext, false
		},
	}
	return mdhtml.NewRenderer(opts)
}
