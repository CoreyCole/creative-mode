package imagegen

import "creative-mode/harness/views/dsutil"

// ImageGenExpr provides typed expression builders for image gen template attributes.
type ImageGenExpr struct {
	s *dsutil.SignalManager
}

var imageGenSignals = dsutil.Signals(struct {
	ImageGenStatus   string `json:"image_gen_status"`   //nolint:tagliatelle // Datastar signal name
	ImageAspectRatio string `json:"image_aspect_ratio"` //nolint:tagliatelle // Datastar signal name
}{})

// IE is the package-level image gen expression helper used in templates.
var IE = NewImageGenExpr(imageGenSignals)

// NewImageGenExpr creates a new ImageGenExpr.
func NewImageGenExpr(s *dsutil.SignalManager) *ImageGenExpr {
	return &ImageGenExpr{s: s}
}

// StatusColorClass returns data-class for status text coloring.
func (e *ImageGenExpr) StatusColorClass() string {
	return e.s.DataClass(map[string]string{
		"text-yellow-400": e.s.Equals("image_gen_status", "generating"),
		"text-green-400":  "$image_gen_status === 'done' || $image_gen_status === 'saved'",
		"text-red-400":    e.s.Equals("image_gen_status", "error"),
	})
}

// AspectRatioActiveClass returns data-class for an aspect ratio button.
func (e *ImageGenExpr) AspectRatioActiveClass(ratio string) string {
	return e.s.DataClass(map[string]string{
		"bg-primary text-primary-foreground": e.s.Equals("image_aspect_ratio", ratio),
		"bg-muted text-muted-foreground":     e.s.NotEquals("image_aspect_ratio", ratio),
	})
}

// SelectAspectRatio returns the expression to set the aspect ratio.
func (e *ImageGenExpr) SelectAspectRatio(ratio string) string {
	return e.s.SetString("image_aspect_ratio", ratio)
}
