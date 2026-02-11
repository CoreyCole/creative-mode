package dsutil

import (
	"fmt"
	"strings"
)

// DatastarExpression is a structured Datastar expression builder.
type DatastarExpression struct {
	statements []string
	separator  string
}

// NewExpression creates a new expression builder with semicolon separator.
func NewExpression() *DatastarExpression {
	return &DatastarExpression{
		statements: make([]string, 0),
		separator:  "; ",
	}
}

// Statement adds a statement to the expression.
func (e *DatastarExpression) Statement(stmt string) *DatastarExpression {
	if stmt != "" {
		e.statements = append(e.statements, stmt)
	}
	return e
}

// SetSignal adds a signal assignment statement.
func (e *DatastarExpression) SetSignal(signal, value string) *DatastarExpression {
	return e.Statement(fmt.Sprintf("$%s = %s", signal, value))
}

// Conditional adds a conditional statement.
func (e *DatastarExpression) Conditional(
	condition, trueExpr, falseExpr string,
) *DatastarExpression {
	if falseExpr == "" {
		falseExpr = "null"
	}
	return e.Statement(fmt.Sprintf("%s ? %s : %s", condition, trueExpr, falseExpr))
}

// Build returns the final expression string.
func (e *DatastarExpression) Build() string {
	if len(e.statements) == 0 {
		return ""
	}
	if len(e.statements) == 1 {
		return e.statements[0]
	}
	if e.separator == ", " {
		return "(" + strings.Join(e.statements, e.separator) + ")"
	}
	return strings.Join(e.statements, e.separator)
}

// BuildConditional creates a conditional expression.
func BuildConditional(condition, trueExpr, falseExpr string) string {
	if falseExpr == "" {
		falseExpr = "null"
	}
	return fmt.Sprintf("%s ? %s : %s", condition, trueExpr, falseExpr)
}
