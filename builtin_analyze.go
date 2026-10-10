// Code-analysis builtins for editor tooling (CUI + GUI).
//
// lextokens() exposes the lexer for syntax highlighting and static
// analysis; strwidth() measures display width for caret placement in
// hand-made editors; parsetree() exposes the parser for outline,
// navigation, and hover.
package main

func init() {
	// lextokens(src): tokenize source, returning an array of
	// [type, text, line, col] arrays (EOF omitted). Lex errors are
	// runtime errors carrying the lexer position.
	register("lextokens", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("lextokens", args, 0, at)
		if err != nil {
			return Null(), err
		}
		toks, err := Lex(src)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		out := make([]Value, 0, len(toks))
		for _, t := range toks {
			if t.Type == TokEOF {
				continue
			}
			out = append(out, ArrayOf([]Value{Str(string(t.Type)), Str(t.Lit), Int(int64(t.Line)), Int(int64(t.Column))}))
		}
		return ArrayOf(out), nil
	})

	// strwidth(s): display width of s — pixels at the current GUI
	// face, rune count on CUI. For caret placement in editors.
	register("strwidth", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strwidth", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(in.be.TextWidth(s))), nil
	})

	// parsetree(src): parse source into a tree for editors (outline,
	// navigation, hover): ["program", line, col, text, stmts].
	// Every node is [type, line, col, text, ...kind fields]; bodies
	// are statement arrays; expression statements unwrap to their
	// expression. Raw directives (#...) are lex errors, like
	// lextokens. Parse errors are runtime errors.
	register("parsetree", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("parsetree", args, 0, at)
		if err != nil {
			return Null(), err
		}
		toks, err := Lex(src)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		prog, err := ParseTokens(toks)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		stmts := make([]Value, 0, len(prog.Stmts))
		for _, s := range prog.Stmts {
			stmts = append(stmts, stmtValue(s))
		}
		return nodeArr("program", Pos{Line: 1, Column: 1}, prog.String(), ArrayOf(stmts)), nil
	})
}

// nodeArr builds the common node array: kind, position, canonical
// text, then kind-specific fields.
func nodeArr(typ string, at Pos, text string, fields ...Value) Value {
	out := make([]Value, 0, 4+len(fields))
	out = append(out, Str(typ), Int(int64(at.Line)), Int(int64(at.Column)), Str(text))
	return ArrayOf(append(out, fields...))
}

// blockValues converts a statement block (nil-safe).
func blockValues(b *BlockStmt) []Value {
	out := make([]Value, 0)
	if b == nil {
		return out
	}
	for _, s := range b.Stmts {
		out = append(out, stmtValue(s))
	}
	return out
}

// exprOrAbsent converts an optional expression. Absence is ""
// (never null): tree slots stay readable into variables, and == ""
// tests them.
func exprOrAbsent(x Expr) Value {
	if x == nil {
		return Str("")
	}
	return exprValue(x)
}

// stmtValue converts one statement; expression statements unwrap to
// their expression so the tree has no "expr" wrapper nodes.
func stmtValue(s Stmt) Value {
	switch n := s.(type) {
	case *AssignStmt:
		return nodeArr("assign", n.Pos(), n.String(), exprValue(n.Target), exprValue(n.Value))
	case *CompoundAssignStmt:
		return nodeArr("compound", n.Pos(), n.String(), exprValue(n.Target), Str(opSymbol(n.Op)), exprValue(n.Value))
	case *ExprStmt:
		return exprValue(n.X)
	case *IfStmt:
		var elseV Value = Str("")
		if n.Else != nil {
			if b, ok := n.Else.(*BlockStmt); ok {
				elseV = ArrayOf(blockValues(b))
			} else {
				elseV = stmtValue(n.Else)
			}
		}
		return nodeArr("if", n.Pos(), n.String(), exprValue(n.Cond), ArrayOf(blockValues(n.Then)), elseV)
	case *RepeatStmt:
		var varV, itemV Value = Str(""), Str("")
		if n.HasVar {
			varV = Str(n.Var)
		}
		if n.HasItem {
			itemV = Str(n.Item)
		}
		return nodeArr("repeat", n.Pos(), n.String(), exprValue(n.Count), varV, itemV, ArrayOf(blockValues(n.Body)))
	case *WhileStmt:
		return nodeArr("while", n.Pos(), n.String(), exprValue(n.Cond), ArrayOf(blockValues(n.Body)))
	case *SwitchStmt:
		cases := make([]Value, 0, len(n.Cases))
		for _, c := range n.Cases {
			vals := make([]Value, 0, len(c.Values))
			for _, v := range c.Values {
				vals = append(vals, exprValue(v))
			}
			cases = append(cases, nodeArr("case", c.At, "", ArrayOf(vals), ArrayOf(blockValues(c.Body)), Bool(c.Default)))
		}
		return nodeArr("switch", n.Pos(), n.String(), exprValue(n.Value), ArrayOf(cases))
	case *BreakStmt:
		return nodeArr("break", n.Pos(), n.String())
	case *ContinueStmt:
		return nodeArr("continue", n.Pos(), n.String())
	case *ReturnStmt:
		return nodeArr("return", n.Pos(), n.String(), exprOrAbsent(n.Value))
	case *DefStmt:
		params := make([]Value, 0, len(n.Params))
		for _, p := range n.Params {
			params = append(params, nodeArr("param", p.At, p.Name, Str(p.Name)))
		}
		return nodeArr("def", n.Pos(), n.String(), Str(n.Name), ArrayOf(params), ArrayOf(blockValues(n.Body)))
	default:
		return nodeArr("unknown", s.Pos(), s.String())
	}
}

// exprValue converts one expression node.
func exprValue(x Expr) Value {
	switch n := x.(type) {
	case *IntLit:
		return nodeArr("int", n.Pos(), n.String(), Int(n.Value))
	case *FloatLit:
		return nodeArr("float", n.Pos(), n.String(), Float(n.Value))
	case *StringLit:
		return nodeArr("string", n.Pos(), n.String(), Str(n.Value))
	case *BoolLit:
		return nodeArr("bool", n.Pos(), n.String(), Bool(n.Value))
	case *NullLit:
		return nodeArr("null", n.Pos(), n.String())
	case *VarExpr:
		return nodeArr("var", n.Pos(), n.String(), Str(n.Name))
	case *ArrayLit:
		elems := make([]Value, 0, len(n.Elems))
		for _, e := range n.Elems {
			elems = append(elems, exprValue(e))
		}
		return nodeArr("array", n.Pos(), n.String(), ArrayOf(elems))
	case *IndexExpr:
		return nodeArr("index", n.Pos(), n.String(), exprValue(n.Base), exprValue(n.Index))
	case *CallExpr:
		args := make([]Value, 0, len(n.Args))
		for _, a := range n.Args {
			args = append(args, exprValue(a))
		}
		return nodeArr("call", n.Pos(), n.String(), exprValue(n.Callee), ArrayOf(args))
	case *UnaryExpr:
		return nodeArr("unary", n.Pos(), n.String(), Str(opSymbol(n.Op)), exprValue(n.X))
	case *BinaryExpr:
		return nodeArr("binary", n.Pos(), n.String(), Str(opSymbol(n.Op)), exprValue(n.L), exprValue(n.R))
	default:
		return nodeArr("unknown", x.Pos(), x.String())
	}
}
