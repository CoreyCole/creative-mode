package tooltip

import (
	"github.com/coreycole/creative-mode/site/internal/ui/utils"
)

// TooltipContentVariants generates CSS classes for tooltip content
func TooltipContentVariants(args TooltipContentArgs) string {
	base := "rounded-md border bg-popover text-popover-foreground shadow-md outline-none px-3 py-1.5 text-sm pointer-events-none"
	return utils.TwMerge(base, args.Class)
}
