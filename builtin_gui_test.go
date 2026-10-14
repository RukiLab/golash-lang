package main

import "testing"

// TestGuiBuiltinsRegisteredはGUI語がguiビルドに存在することを検証する。
func TestGuiBuiltinsRegistered(t *testing.T) {
	for _, n := range guiBuiltinNames {
		if !isBuiltin(n) {
			t.Errorf("gui builtin %q not registered", n)
		}
	}
}
