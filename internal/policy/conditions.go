package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"handoffguard/internal/envelope"
)

// Conditions compiles and evaluates approval conditions. Context roots contain
// request data, never evidence that an approval has actually been granted.
type Conditions struct{ env *cel.Env }

func NewConditions() (*Conditions, error) {
	opts := []cel.EnvOption{}
	for _, name := range []string{"refund", "order", "customer", "request"} {
		opts = append(opts, cel.Variable(name, cel.MapType(cel.StringType, cel.DynType)))
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	return &Conditions{env: env}, nil
}

func (c *Conditions) compile(condition string) (*cel.Ast, error) {
	if len(condition) > 4096 {
		return nil, fmt.Errorf("condition exceeds 4096 bytes")
	}
	ast, issues := c.env.Compile(condition)
	if issues.Err() != nil {
		return nil, issues.Err()
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("condition must return bool, got %s", ast.OutputType())
	}
	return ast, nil
}

// RequiredRoles fails closed on malformed conditions, missing data, and CEL
// errors. Callers must deny on error; an empty result on error is not permission.
func (c *Conditions) RequiredRoles(approvals []envelope.Approval, context map[string]any) ([]string, error) {
	roles := map[string]bool{}
	for _, approval := range approvals {
		if strings.TrimSpace(approval.RequiredRole) == "" {
			return nil, fmt.Errorf("missing required role")
		}
		ast, err := c.compile(approval.Condition)
		if err != nil {
			return nil, fmt.Errorf("approval %q: %w", approval.RequiredRole, err)
		}
		program, err := c.env.Program(ast, cel.CostLimit(10000))
		if err != nil {
			return nil, err
		}
		value, _, err := program.Eval(context)
		if err != nil {
			return nil, fmt.Errorf("approval %q: %w", approval.RequiredRole, err)
		}
		b, ok := value.(types.Bool)
		if !ok {
			return nil, fmt.Errorf("approval did not evaluate to bool")
		}
		if b == types.True {
			roles[approval.RequiredRole] = true
		}
	}
	result := make([]string, 0, len(roles))
	for role := range roles {
		result = append(result, role)
	}
	sort.Strings(result)
	return result, nil
}

// preserves proves parent => child for identical conditions, unconditional
// approvals, and simple integer threshold comparisons on the same field.
// General CEL implication is deliberately not guessed or sampled.
func preserves(parent, child *cel.Ast) bool {
	p, _ := cel.AstToString(parent)
	c, _ := cel.AstToString(child)
	if p == c || c == "true" || p == "false" {
		return true
	}
	pf, po, pn, pok := threshold(parent.Expr())
	cf, co, cn, cok := threshold(child.Expr())
	if !pok || !cok || pf != cf {
		return false
	}
	if (po == "_>_" || po == "_>=_") && (co == "_>_" || co == "_>=_") {
		return cn < pn || (cn == pn && (po == "_>_" || co == "_>=_"))
	}
	if (po == "_<_" || po == "_<=_") && (co == "_<_" || co == "_<=_") {
		return cn > pn || (cn == pn && (po == "_<_" || co == "_<=_"))
	}
	return false
}

func threshold(e *exprpb.Expr) (string, string, int64, bool) {
	call := e.GetCallExpr()
	if call == nil || len(call.Args) != 2 || call.Target != nil {
		return "", "", 0, false
	}
	field := fieldPath(call.Args[0])
	literal := call.Args[1].GetConstExpr()
	if field == "" || literal == nil {
		return "", "", 0, false
	}
	n, ok := literal.ConstantKind.(*exprpb.Constant_Int64Value)
	if !ok {
		return "", "", 0, false
	}
	return field, call.Function, n.Int64Value, true
}

func fieldPath(e *exprpb.Expr) string {
	if id := e.GetIdentExpr(); id != nil {
		return id.Name
	}
	if s := e.GetSelectExpr(); s != nil && !s.TestOnly {
		if prefix := fieldPath(s.Operand); prefix != "" {
			return prefix + "." + s.Field
		}
	}
	return ""
}
