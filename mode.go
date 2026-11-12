// 実行モード（CUI / GUI）の自動判定。
//
// main から到達可能なコードに GUI 専用組込みがあるかで判定します。
// include されているだけで呼ばれない GUI 関数は無視するため、GUI ヘルパを
// 含むライブラリを CLI ツールから include しても CUI のまま動きます。
// モードを固定する宣言はありません。
package main

// guiModeBuiltins は GUI バックエンド（ウィンドウ）を必要とする組込みです。
// これらは CUI で呼ぶと「GUI ビルドが必要です」エラーになるため、到達可能な
// コードに1つでもあれば GUI と判定します。mes / print / input / pos / color
// など両方で動くものは含めません。
var guiModeBuiltins = map[string]bool{
	"screen": true, "gsel": true,
	"pset": true, "pget": true, "line": true, "rect": true, "ellipse": true,
	"gcopy": true, "gblit": true, "gmode": true, "picload": true,
	"pngsave": true, "galpha": true, "font": true,
	"gquad": true, "tri": true,
	"getkey": true, "mouse": true, "mousewheel": true,
	"fullscreen": true, "closing": true, "cursor": true,
	"padcount": true, "padbtn": true, "padaxis": true, "padname": true,
	"resizable": true, "touch": true,
	"dropfiles": true, "dropload": true,
	"mmload": true, "mmplay": true, "mmstop": true, "mmvol": true, "mmpitch": true,
	"button": true, "inputbox": true, "ime": true,
	"listbox": true, "clrobj": true, "dialog": true,
	"chkbox": true, "combox": true, "mesbox": true,
	"objset": true, "objget": true, "objenable": true,
	"wbind": true, "wbind_items": true, "wunbind": true, "wsync": true,
}

// isFileDialogCall は dialog(filter, 3|4) の形（第2引数が 3/4 の整数
// リテラル）を報告する。ファイル選択は CUI でも動くため GUI 判定から外す。
// 変数・省略・0/1 はメッセージ系として従来通り GUI 扱いにする。
func isFileDialogCall(name string, n *CallExpr) bool {
	if name != "dialog" || len(n.Args) != 2 {
		return false
	}
	lit, ok := n.Args[1].(*IntLit)
	if !ok {
		return false
	}
	return lit.Value == 3 || lit.Value == 4
}

// DetectRunMode は main から到達可能なコードだけをたどり、GUI 専用組込みが
// 現れるかを調べます。トップレベルの def 本体は、その関数が呼ばれたときに
// だけ探索します。
func DetectRunMode(prog *Program) string {
	if prog == nil {
		return "cli"
	}
	defs := map[string]*DefStmt{}
	for _, s := range prog.Stmts {
		if d, ok := s.(*DefStmt); ok {
			defs[d.Name] = d
		}
	}
	visited := map[string]bool{}
	usesGUI := false

	var walkExpr func(Expr)
	var walkStmt func(Stmt)

	walkExpr = func(x Expr) {
		switch n := x.(type) {
		case nil:
			return
		case *VarExpr:
		case *ArrayLit:
			for _, e := range n.Elems {
				walkExpr(e)
			}
		case *IndexExpr:
			walkExpr(n.Base)
			walkExpr(n.Index)
		case *CallExpr:
			if v, ok := n.Callee.(*VarExpr); ok {
				if guiModeBuiltins[v.Name] && !isFileDialogCall(v.Name, n) {
					usesGUI = true
				}
				if d, ok := defs[v.Name]; ok && !visited[v.Name] {
					visited[v.Name] = true
					walkStmt(d.Body)
				}
			} else {
				walkExpr(n.Callee)
			}
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *UnaryExpr:
			walkExpr(n.X)
		case *BinaryExpr:
			walkExpr(n.L)
			walkExpr(n.R)
		}
	}

	walkStmt = func(s Stmt) {
		switch n := s.(type) {
		case nil:
			return
		case *ExprStmt:
			walkExpr(n.X)
		case *AssignStmt:
			walkExpr(n.Target)
			walkExpr(n.Value)
		case *VarStmt:
			walkExpr(n.Value)
		case *CompoundAssignStmt:
			walkExpr(n.Target)
			walkExpr(n.Value)
		case *IfStmt:
			walkExpr(n.Cond)
			walkStmt(n.Then)
			walkStmt(n.Else)
		case *RepeatStmt:
			walkExpr(n.Count)
			walkStmt(n.Body)
		case *WhileStmt:
			walkExpr(n.Cond)
			walkStmt(n.Body)
		case *SwitchStmt:
			walkExpr(n.Value)
			for _, c := range n.Cases {
				for _, v := range c.Values {
					walkExpr(v)
				}
				walkStmt(c.Body)
			}
		case *ReturnStmt:
			walkExpr(n.Value)
		case *BlockStmt:
			for _, q := range n.Stmts {
				walkStmt(q)
			}
		case *DefStmt:
			// 呼ばれたときだけ walkExpr 側から辿る。
		case *IncludeStmt, *DllStmt, *BreakStmt, *ContinueStmt:
		}
	}

	for _, s := range prog.Stmts {
		if _, ok := s.(*DefStmt); ok {
			continue
		}
		walkStmt(s)
	}
	if usesGUI {
		return "gui"
	}
	return "cli"
}
