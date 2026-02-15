package tooltip

import (
	"fmt"

	"github.com/coreycole/creative-mode/site/internal/ui/utils"
)

// TooltipHandler provides methods for building tooltip-related expressions
type TooltipHandler struct {
	tooltipID string
}

// NewTooltipHandler creates a new tooltip handler
func NewTooltipHandler(tooltipID string) *TooltipHandler {
	return &TooltipHandler{
		tooltipID: tooltipID,
	}
}

// BuildShowHandler creates the hover/focus handler to show tooltip
func (h *TooltipHandler) BuildShowHandler(delayMs int) string {
	return fmt.Sprintf("setTimeout(() => { document.getElementById('%s').showPopover(); }, %d)", h.tooltipID, delayMs)
}

// BuildHideHandler creates the handler to hide tooltip
func (h *TooltipHandler) BuildHideHandler() string {
	return fmt.Sprintf("document.getElementById('%s').hidePopover()", h.tooltipID)
}

// BuildInstantShowHandler creates handler to show tooltip without delay
func (h *TooltipHandler) BuildInstantShowHandler() string {
	signals := utils.Signals(h.tooltipID, TooltipSignals{})

	return utils.NewExpression().
		Statement("clearTimeout($" + h.tooltipID + ".hideTimeout)").
		Statement("clearTimeout($" + h.tooltipID + ".showTimeout)").
		Statement("$" + h.tooltipID + ".hideTimeout = null").
		Statement("$" + h.tooltipID + ".showTimeout = null").
		Statement(signals.Set("open", "true")).
		Build()
}

// BuildDelayedHideHandler creates handler to hide tooltip with delay
func (h *TooltipHandler) BuildDelayedHideHandler(delayMs int) string {
	return utils.NewExpression().
		Statement("clearTimeout($" + h.tooltipID + ".showTimeout)").
		Statement("$" + h.tooltipID + ".showTimeout = null").
		Statement("$" + h.tooltipID + ".hideTimeout = setTimeout(() => { $" + h.tooltipID + ".open = false; }, " + fmt.Sprintf("%d", delayMs) + ")").
		Build()
}

// BuildAnchorStyle creates anchor positioning style
func (h *TooltipHandler) BuildAnchorStyle(anchorName string) string {
	if anchorName == "" {
		return ""
	}
	return fmt.Sprintf("anchor-name: --%s", anchorName)
}

// BuildPositionAnchorStyle creates position anchor style
func (h *TooltipHandler) BuildPositionAnchorStyle(anchorName string) string {
	if anchorName == "" {
		return ""
	}
	return fmt.Sprintf("position-anchor: --%s", anchorName)
}

// BuildTouchStartHandler creates handler for touch start (mobile touch-and-hold)
func (h *TooltipHandler) BuildTouchStartHandler(touchHoldMs int) string {
	if touchHoldMs == 0 {
		touchHoldMs = 500
	}

	return utils.NewExpression().
		Statement("evt.preventDefault()").
		Statement("clearTimeout($" + h.tooltipID + ".touchTimer)").
		Statement("$" + h.tooltipID + ".touchTimer = setTimeout(() => { " +
			"$" + h.tooltipID + ".touchHeld = true; " +
			"document.getElementById('" + h.tooltipID + "').showPopover(); " +
			"}, " + fmt.Sprintf("%d", touchHoldMs) + ")").
		Build()
}

// BuildTouchEndHandler creates handler for touch end (cancel if released early)
func (h *TooltipHandler) BuildTouchEndHandler() string {
	return utils.NewExpression().
		Statement("clearTimeout($"+h.tooltipID+".touchTimer)").
		Statement("$"+h.tooltipID+".touchTimer = null").
		Conditional(
			"!$"+h.tooltipID+".touchHeld",
			"document.getElementById('"+h.tooltipID+"').hidePopover()",
			"null",
		).
		Build()
}

// BuildClickOutsideHandler creates handler to dismiss tooltip when clicking outside
func (h *TooltipHandler) BuildClickOutsideHandler(triggerSelector string) string {
	condition := "$" + h.tooltipID + ".touchHeld && !evt.target.closest('" + triggerSelector + "')"
	action := "($" + h.tooltipID + ".touchHeld = false, document.getElementById('" + h.tooltipID + "').hidePopover())"
	return utils.BuildConditional(condition, action, "null")
}
