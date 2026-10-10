// Lexer for HSP successor language v0.2.
//
// Statements are separated by NEWLINE. A ';' is lexed as SEMICOLON so the
// parser can reject it with a clear error (v0.2: semicolons are forbidden).
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// TokenType identifies a lexical token.
type TokenType string

const (
	TokEOF       TokenType = "EOF"
	TokNewline   TokenType = "NEWLINE"
	TokSemicolon TokenType = "SEMICOLON"

	TokIdent  TokenType = "IDENT"
	TokInt    TokenType = "INT"
	TokFloat  TokenType = "FLOAT"
	TokString TokenType = "STRING"
	TokTrue   TokenType = "TRUE"
	TokFalse  TokenType = "FALSE"

	TokDef      TokenType = "DEF"
	TokIf       TokenType = "IF"
	TokElse     TokenType = "ELSE"
	TokRepeat   TokenType = "REPEAT"
	TokWhile    TokenType = "WHILE"
	TokSwitch   TokenType = "SWITCH"
	TokCase     TokenType = "CASE"
	TokDefault  TokenType = "DEFAULT"
	TokAs       TokenType = "AS"
	TokBreak    TokenType = "BREAK"
	TokContinue TokenType = "CONTINUE"
	TokReturn   TokenType = "RETURN"

	TokPlus         TokenType = "PLUS"         // +
	TokMinus        TokenType = "MINUS"        // -
	TokStar         TokenType = "STAR"         // *
	TokSlash        TokenType = "SLASH"        // /
	TokMod          TokenType = "MOD"          // %
	TokAssign       TokenType = "ASSIGN"       // =
	TokPlusAssign   TokenType = "PLUSASSIGN"   // +=
	TokMinusAssign  TokenType = "MINUSASSIGN"  // -=
	TokStarAssign   TokenType = "STARASSIGN"   // *=
	TokSlashAssign  TokenType = "SLASHASSIGN"  // /=
	TokModAssign    TokenType = "MODASSIGN"    // %=
	TokBitAndAssign TokenType = "BITANDASSIGN" // &=
	TokBitOrAssign  TokenType = "BITORASSIGN"  // |=
	TokBitXorAssign TokenType = "BITXORASSIGN" // ^=
	TokShlAssign    TokenType = "SHLASSIGN"    // <<=
	TokShrAssign    TokenType = "SHRASSIGN"    // >>=
	TokEq           TokenType = "EQ"           // ==
	TokBang         TokenType = "BANG"         // !
	TokNotEq        TokenType = "NOTEQ"        // !=
	TokLt           TokenType = "LT"           // <
	TokLtEq         TokenType = "LTE"          // <=
	TokGt           TokenType = "GT"           // >
	TokGtEq         TokenType = "GTE"          // >=
	TokAnd          TokenType = "AND"          // &&
	TokOr           TokenType = "OR"           // ||
	TokBitAnd       TokenType = "BITAND"       // &
	TokBitOr        TokenType = "BITOR"        // |
	TokBitXor       TokenType = "BITXOR"       // ^
	TokBitNot       TokenType = "BITNOT"       // ~
	TokShl          TokenType = "SHL"          // <<
	TokShr          TokenType = "SHR"          // >>

	TokComma    TokenType = "COMMA"    // ,
	TokColon    TokenType = "COLON"    // :
	TokDot      TokenType = "DOT"      // .
	TokLParen   TokenType = "LPAREN"   // (
	TokRParen   TokenType = "RPAREN"   // )
	TokLBracket TokenType = "LBRACKET" // [
	TokRBracket TokenType = "RBRACKET" // ]
	TokLBrace   TokenType = "LBRACE"   // {
	TokRBrace   TokenType = "RBRACE"   // }
)

// Token is a single lexical unit with source position.
type Token struct {
	Type   TokenType
	Lit    string
	File   string // display path; "" for single-source inputs
	Line   int    // 1-based, file-local
	Column int    // 1-based
}

func (t Token) String() string {
	if t.File != "" {
		return fmt.Sprintf("%s(%q)@%s:%d:%d", t.Type, t.Lit, t.File, t.Line, t.Column)
	}
	return fmt.Sprintf("%s(%q)@%d:%d", t.Type, t.Lit, t.Line, t.Column)
}

// LexError is a lexical error with position.
type LexError struct {
	File   string
	Line   int
	Column int
	Msg    string
}

func (e *LexError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Msg)
	}
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Msg)
}

var keywords = map[string]TokenType{
	"def":      TokDef,
	"if":       TokIf,
	"else":     TokElse,
	"repeat":   TokRepeat,
	"while":    TokWhile,
	"switch":   TokSwitch,
	"case":     TokCase,
	"default":  TokDefault,
	"as":       TokAs,
	"break":    TokBreak,
	"continue": TokContinue,
	"return":   TokReturn,
	"true":     TokTrue,
	"false":    TokFalse,
}

// Lex tokenizes src. It returns the token slice (ending with EOF) or an error.
func Lex(src string) ([]Token, error) {
	l := &lexer{runes: []rune(src), line: 1, col: 1}
	var toks []Token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.Type == TokEOF {
			return toks, nil
		}
	}
}

type lexer struct {
	runes []rune
	pos   int
	line  int
	col   int
}

func (l *lexer) eof() bool { return l.pos >= len(l.runes) }

func (l *lexer) peek() rune {
	if l.eof() {
		return 0
	}
	return l.runes[l.pos]
}

func (l *lexer) peekAt(n int) rune {
	if l.pos+n >= len(l.runes) {
		return 0
	}
	return l.runes[l.pos+n]
}

func (l *lexer) errf(format string, args ...any) error {
	return &LexError{Line: l.line, Column: l.col, Msg: fmt.Sprintf(format, args...)}
}

func (l *lexer) advance() rune {
	c := l.runes[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func isDigit(c rune) bool { return '0' <= c && c <= '9' }
func isAlpha(c rune) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || c == '_'
}
func isAlnum(c rune) bool { return isAlpha(c) || isDigit(c) }

func (l *lexer) next() (Token, error) {
	// Skip blanks (but not newlines).
	for !l.eof() {
		c := l.peek()
		if c == ' ' || c == '\t' || c == '\r' {
			l.advance()
			continue
		}
		// Line comment.
		if c == '/' && l.peekAt(1) == '/' {
			for !l.eof() && l.peek() != '\n' {
				if l.peek() == 0 {
					return Token{}, &LexError{Line: l.line, Column: l.col, Msg: "不正な文字 \"\\0\" です"}
				}
				l.advance()
			}
			continue
		}
		// Block comment (nestable).
		if c == '/' && l.peekAt(1) == '*' {
			if err := l.skipBlockComment(); err != nil {
				return Token{}, err
			}
			continue
		}
		break
	}

	if l.eof() {
		return Token{Type: TokEOF, Line: l.line, Column: l.col}, nil
	}

	line, col := l.line, l.col
	c := l.peek()

	// A raw NUL byte is never valid source text (the `\0` escape is
	// the only way to spell one). peek() also returns 0 at EOF, but
	// that case returned above.
	if c == 0 {
		return Token{}, &LexError{Line: line, Column: col, Msg: "不正な文字 \"\\0\" です"}
	}

	// Newline = statement separator.
	if c == '\n' {
		l.advance()
		return Token{Type: TokNewline, Lit: "\n", Line: line, Column: col}, nil
	}

	// String literal.
	if c == '"' {
		return l.readString()
	}

	// Number (including leading-dot floats like .5).
	if isDigit(c) || (c == '.' && isDigit(l.peekAt(1))) {
		return l.readNumber()
	}

	// Identifier / keyword.
	if isAlpha(c) {
		start := l.pos
		for !l.eof() && isAlnum(l.peek()) {
			l.advance()
		}
		lit := string(l.runes[start:l.pos])
		if kw, ok := keywords[lit]; ok {
			return Token{Type: kw, Lit: lit, Line: line, Column: col}, nil
		}
		return Token{Type: TokIdent, Lit: lit, Line: line, Column: col}, nil
	}

	// Three-character operators first (<<= >>=).
	three := string([]rune{c, l.peekAt(1), l.peekAt(2)})
	switch three {
	case "<<=":
		l.advance()
		l.advance()
		l.advance()
		return Token{Type: TokShlAssign, Lit: "<<=", Line: line, Column: col}, nil
	case ">>=":
		l.advance()
		l.advance()
		l.advance()
		return Token{Type: TokShrAssign, Lit: ">>=", Line: line, Column: col}, nil
	}

	// Two-character operators first.
	two := string([]rune{c, l.peekAt(1)})
	switch two {
	case "==":
		l.advance()
		l.advance()
		return Token{Type: TokEq, Lit: "==", Line: line, Column: col}, nil
	case "!=":
		l.advance()
		l.advance()
		return Token{Type: TokNotEq, Lit: "!=", Line: line, Column: col}, nil
	case "<=":
		l.advance()
		l.advance()
		return Token{Type: TokLtEq, Lit: "<=", Line: line, Column: col}, nil
	case ">=":
		l.advance()
		l.advance()
		return Token{Type: TokGtEq, Lit: ">=", Line: line, Column: col}, nil
	case "&&":
		l.advance()
		l.advance()
		return Token{Type: TokAnd, Lit: "&&", Line: line, Column: col}, nil
	case "||":
		l.advance()
		l.advance()
		return Token{Type: TokOr, Lit: "||", Line: line, Column: col}, nil
	case "<<":
		l.advance()
		l.advance()
		return Token{Type: TokShl, Lit: "<<", Line: line, Column: col}, nil
	case ">>":
		l.advance()
		l.advance()
		return Token{Type: TokShr, Lit: ">>", Line: line, Column: col}, nil
	case "+=":
		l.advance()
		l.advance()
		return Token{Type: TokPlusAssign, Lit: "+=", Line: line, Column: col}, nil
	case "-=":
		l.advance()
		l.advance()
		return Token{Type: TokMinusAssign, Lit: "-=", Line: line, Column: col}, nil
	case "*=":
		l.advance()
		l.advance()
		return Token{Type: TokStarAssign, Lit: "*=", Line: line, Column: col}, nil
	case "/=":
		l.advance()
		l.advance()
		return Token{Type: TokSlashAssign, Lit: "/=", Line: line, Column: col}, nil
	case "%=":
		l.advance()
		l.advance()
		return Token{Type: TokModAssign, Lit: "%=", Line: line, Column: col}, nil
	case "&=":
		l.advance()
		l.advance()
		return Token{Type: TokBitAndAssign, Lit: "&=", Line: line, Column: col}, nil
	case "|=":
		l.advance()
		l.advance()
		return Token{Type: TokBitOrAssign, Lit: "|=", Line: line, Column: col}, nil
	case "^=":
		l.advance()
		l.advance()
		return Token{Type: TokBitXorAssign, Lit: "^=", Line: line, Column: col}, nil
	}

	// Single-character tokens.
	l.advance()
	switch c {
	case '+':
		return Token{Type: TokPlus, Lit: "+", Line: line, Column: col}, nil
	case '-':
		return Token{Type: TokMinus, Lit: "-", Line: line, Column: col}, nil
	case '*':
		return Token{Type: TokStar, Lit: "*", Line: line, Column: col}, nil
	case '/':
		return Token{Type: TokSlash, Lit: "/", Line: line, Column: col}, nil
	case '%':
		return Token{Type: TokMod, Lit: "%", Line: line, Column: col}, nil
	case '=':
		return Token{Type: TokAssign, Lit: "=", Line: line, Column: col}, nil
	case '!':
		return Token{Type: TokBang, Lit: "!", Line: line, Column: col}, nil
	case '<':
		return Token{Type: TokLt, Lit: "<", Line: line, Column: col}, nil
	case '>':
		return Token{Type: TokGt, Lit: ">", Line: line, Column: col}, nil
	case ',':
		return Token{Type: TokComma, Lit: ",", Line: line, Column: col}, nil
	case ':':
		return Token{Type: TokColon, Lit: ":", Line: line, Column: col}, nil
	case '.':
		return Token{Type: TokDot, Lit: ".", Line: line, Column: col}, nil
	case '(':
		return Token{Type: TokLParen, Lit: "(", Line: line, Column: col}, nil
	case ')':
		return Token{Type: TokRParen, Lit: ")", Line: line, Column: col}, nil
	case '[':
		return Token{Type: TokLBracket, Lit: "[", Line: line, Column: col}, nil
	case ']':
		return Token{Type: TokRBracket, Lit: "]", Line: line, Column: col}, nil
	case '{':
		return Token{Type: TokLBrace, Lit: "{", Line: line, Column: col}, nil
	case '}':
		return Token{Type: TokRBrace, Lit: "}", Line: line, Column: col}, nil
	case ';':
		return Token{Type: TokSemicolon, Lit: ";", Line: line, Column: col}, nil
	case '&':
		return Token{Type: TokBitAnd, Lit: "&", Line: line, Column: col}, nil
	case '|':
		return Token{Type: TokBitOr, Lit: "|", Line: line, Column: col}, nil
	case '^':
		return Token{Type: TokBitXor, Lit: "^", Line: line, Column: col}, nil
	case '~':
		return Token{Type: TokBitNot, Lit: "~", Line: line, Column: col}, nil
	case '\'':
		return Token{}, &LexError{Line: line, Column: col, Msg: "シングルクォートの文字列はサポートされていません。ダブルクォートを使用してください"}
	}
	return Token{}, &LexError{Line: line, Column: col, Msg: fmt.Sprintf("不正な文字 %q です", string(c))}
}

func (l *lexer) skipBlockComment() error {
	startLine, startCol := l.line, l.col
	// Consume "/*".
	l.advance()
	l.advance()
	depth := 1
	for !l.eof() {
		if l.peek() == 0 {
			return &LexError{Line: l.line, Column: l.col, Msg: "不正な文字 \"\\0\" です"}
		}
		if l.peek() == '/' && l.peekAt(1) == '*' {
			l.advance()
			l.advance()
			depth++
			continue
		}
		if l.peek() == '*' && l.peekAt(1) == '/' {
			l.advance()
			l.advance()
			depth--
			if depth == 0 {
				return nil
			}
			continue
		}
		l.advance()
	}
	return &LexError{Line: startLine, Column: startCol, Msg: "ブロックコメントが閉じられていません"}
}

func (l *lexer) readString() (Token, error) {
	line, col := l.line, l.col
	l.advance() // opening quote
	var sb strings.Builder
	for {
		if l.eof() {
			return Token{}, &LexError{Line: line, Column: col, Msg: "文字列リテラルが閉じられていません"}
		}
		c := l.peek()
		if c == 0 {
			return Token{}, &LexError{Line: l.line, Column: l.col, Msg: "不正な文字 \"\\0\" です"}
		}
		if c == '\n' {
			return Token{}, &LexError{Line: line, Column: col, Msg: "文字列リテラルが閉じられていません（文字列内で改行しています）"}
		}
		if c == '"' {
			l.advance()
			return Token{Type: TokString, Lit: sb.String(), Line: line, Column: col}, nil
		}
		if c == '\\' {
			l.advance()
			if l.eof() {
				return Token{}, &LexError{Line: line, Column: col, Msg: "文字列リテラルが閉じられていません"}
			}
			e := l.advance()
			switch e {
			case '"':
				sb.WriteRune('"')
			case '\\':
				sb.WriteRune('\\')
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case '0':
				sb.WriteRune(0)
			default:
				return Token{}, &LexError{Line: l.line, Column: l.col - 1, Msg: fmt.Sprintf("不明なエスケープシーケンス '\\%c' です", e)}
			}
			continue
		}
		sb.WriteRune(c)
		l.advance()
	}
}

func (l *lexer) readNumber() (Token, error) {
	line, col := l.line, l.col
	if l.peek() == '0' {
		if base, name, ok := radixPrefix(l.peekAt(1)); ok {
			return l.readRadix(line, col, base, name)
		}
	}
	start := l.pos
	for !l.eof() && isDigit(l.peek()) {
		l.advance()
	}
	isFloat := false
	// `3.` is not a float: a dot must be followed by a digit here.
	// (A leading-dot float like `.5` enters through the caller.)
	if l.peek() == '.' && !isDigit(l.peekAt(1)) {
		return Token{}, &LexError{Line: line, Column: col, Msg: "不正な数値リテラルです"}
	}
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		isFloat = true
		l.advance() // dot
		for !l.eof() && isDigit(l.peek()) {
			l.advance()
		}
	}
	if l.peek() == 'e' || l.peek() == 'E' {
		l.advance()
		if l.peek() == '+' || l.peek() == '-' {
			l.advance()
		}
		if !isDigit(l.peek()) {
			// `10else` のような英字接着は指数エラーではなく
			// 不正な数値リテラル（一族: 1_000・123abc）として
			// 先頭位置で報告する。
			if c := l.peek(); isAlpha(c) || c == '_' {
				return Token{}, &LexError{Line: line, Column: col, Msg: "不正な数値リテラルです"}
			}
			return Token{}, &LexError{Line: l.line, Column: l.col, Msg: "数値の指数部の形式が不正です"}
		}
		isFloat = true
		for !l.eof() && isDigit(l.peek()) {
			l.advance()
		}
	}
	// Digits glued to letters or `_` (1_000, 123abc, 0d5) are a
	// malformed number, not a number plus an identifier.
	if c := l.peek(); isAlpha(c) || c == '_' {
		return Token{}, &LexError{Line: line, Column: col, Msg: "不正な数値リテラルです"}
	}
	lit := string(l.runes[start:l.pos])
	if isFloat {
		return Token{Type: TokFloat, Lit: lit, Line: line, Column: col}, nil
	}
	return Token{Type: TokInt, Lit: lit, Line: line, Column: col}, nil
}

// radixPrefix maps a radix letter to its base (0x/0b/0o, either case).
func radixPrefix(c rune) (int, string, bool) {
	switch c {
	case 'x', 'X':
		return 16, "16進数", true
	case 'b', 'B':
		return 2, "2進数", true
	case 'o', 'O':
		return 8, "8進数", true
	}
	return 0, "", false
}

func radixDigit(c rune, base int) bool {
	switch {
	case '0' <= c && c <= '9':
		return int(c-'0') < base
	case base == 16 && 'a' <= c && c <= 'f':
		return true
	case base == 16 && 'A' <= c && c <= 'F':
		return true
	}
	return false
}

// readRadix lexes a 0x/0b/0o integer literal, normalizing Lit to decimal
// so the parser needs no base-aware path. Position points at the `0`.
func (l *lexer) readRadix(line, col, base int, name string) (Token, error) {
	l.advance() // 0
	l.advance() // prefix letter
	start := l.pos
	for !l.eof() && radixDigit(l.peek(), base) {
		l.advance()
	}
	digits := string(l.runes[start:l.pos])
	if digits == "" {
		return Token{}, &LexError{Line: line, Column: col, Msg: fmt.Sprintf("不正な%sリテラルです", name)}
	}
	if c := l.peek(); isAlnum(c) || c == '_' {
		return Token{}, &LexError{Line: line, Column: col, Msg: fmt.Sprintf("不正な%sリテラルです", name)}
	}
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		return Token{}, &LexError{Line: line, Column: col, Msg: fmt.Sprintf("%sの浮動小数はサポートされていません", name)}
	}
	// `3.` が不正な数値リテラルになるのと同様、基数リテラルの直後の
	// `.` も不正とする（`0x1.` → INT DOT ではなく字句エラー）。
	if l.peek() == '.' {
		return Token{}, &LexError{Line: line, Column: col, Msg: fmt.Sprintf("不正な%sリテラルです", name)}
	}
	v, err := strconv.ParseInt(digits, base, 64)
	if err != nil {
		// Exactly 2^63 is allowed through as decimal so unary minus
		// can fold it to MinInt64; the bare positive literal still
		// fails in the parser.
		if u, uerr := strconv.ParseUint(digits, base, 64); uerr == nil && u == 1<<63 {
			return Token{Type: TokInt, Lit: "9223372036854775808", Line: line, Column: col}, nil
		}
		return Token{}, &LexError{Line: line, Column: col, Msg: fmt.Sprintf("%sリテラルが範囲外です", name)}
	}
	return Token{Type: TokInt, Lit: strconv.FormatInt(v, 10), Line: line, Column: col}, nil
}
