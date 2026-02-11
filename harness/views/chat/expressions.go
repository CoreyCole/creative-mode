package chat

import "creative-mode/harness/views/dsutil"

// ChatExpr provides typed expression builders for chat template attributes.
type ChatExpr struct {
	s *dsutil.SignalManager
}

var chatSignals = dsutil.Signals(struct {
	ActiveTab string `json:"active_tab"` //nolint:tagliatelle // Datastar signal name
}{})

// CE is the package-level chat expression helper used in templates.
var CE = NewChatExpr(chatSignals)

// NewChatExpr creates a new ChatExpr.
func NewChatExpr(s *dsutil.SignalManager) *ChatExpr {
	return &ChatExpr{s: s}
}

// SelectTab returns the expression to switch to a given tab.
func (c *ChatExpr) SelectTab(tab string) string {
	return c.s.SetString("active_tab", tab)
}

// SelectLineageTab returns the expression to switch to lineage tab and load data.
func (c *ChatExpr) SelectLineageTab() string {
	return dsutil.NewExpression().
		Statement(c.s.SetString("active_tab", "lineage")).
		Statement("loadLineage($current_world_id, $current_checkpoint_id)").
		Build()
}

// TabActiveClass returns the data-class expression for tab active styling.
func (c *ChatExpr) TabActiveClass(tab string) string {
	return c.s.DataClass(map[string]string{
		"tab-active": c.s.Equals("active_tab", tab),
	})
}
