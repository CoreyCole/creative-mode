package world

import "creative-mode/harness/views/dsutil"

// OverlayExpr provides typed expression builders for overlay template attributes.
type OverlayExpr struct {
	s *dsutil.SignalManager
}

var overlaySignals = dsutil.Signals(OverlaySignals{})

// OE is the package-level overlay expression helper used in templates.
var OE = NewOverlayExpr(overlaySignals)

// NewOverlayExpr creates a new OverlayExpr.
func NewOverlayExpr(s *dsutil.SignalManager) *OverlayExpr {
	return &OverlayExpr{s: s}
}

// Expand returns the expression to expand the overlay and reset unread count.
func (o *OverlayExpr) Expand() string {
	return dsutil.NewExpression().
		Statement(o.s.Set("overlay_expanded", "true")).
		Statement(o.s.Set("unread_count", "0")).
		Build()
}

// Minimize returns the expression to minimize the overlay.
func (o *OverlayExpr) Minimize() string {
	return o.s.Set("overlay_expanded", "false")
}

// ToggleTree returns the expression to toggle checkpoint tree visibility.
func (o *OverlayExpr) ToggleTree() string {
	return o.s.Toggle("show_checkpoint_tree")
}

// BuildStatusDataClass returns the data-class expression for build status styling.
func (o *OverlayExpr) BuildStatusDataClass() string {
	return o.s.DataClass(map[string]string{
		"text-muted-foreground": o.s.Equals("build_status", "idle"),
		"text-amber-500":        o.s.Equals("build_status", "editing"),
		"text-blue-500":         o.s.Equals("build_status", "compiling"),
		"text-green-500":        o.s.Equals("build_status", "ready"),
		"text-red-500":          o.s.Equals("build_status", "failed"),
		"text-orange-500":       o.s.Equals("build_status", "rate_limited"),
	})
}
