package parse

import "testing"

func TestParseBlock(t *testing.T) {
	tests := []struct {
		name     string
		block    string
		wantCmds []string
	}{
		{"empty", "", nil},
		{"no commands", "just some text\nnothing here", nil},
		{"single command", "\n§ ls -la\n", []string{"ls -la"}},
		{"two commands", "\n§ pwd\n§ ls -la\n", []string{"pwd", "ls -la"}},
		{"heredoc command", "\n§ cat <<'EOF'\nline1\nEOF\n§ ls\n", []string{"cat <<'EOF'\nline1\nEOF", "ls"}},
		{"stray text between barriers included", "some text\n§ pwd\nmore text\n§ ls\n", []string{"pwd\nmore text", "ls"}},
		{"heredoc with dash", "\n§ cat <<-EOF\n\tindented\nEOF\n", []string{"cat <<-EOF\n\tindented\nEOF"}},
		{"unclosed heredoc", "\n§ cat <<'EOF'\nline1\nline2", []string{"cat <<'EOF'\nline1\nline2"}},
		{"prefix only no args", "\n§ \n§ ls\n", []string{"ls"}},
		{
			"§ inside heredoc becomes separate command",
			"§ cat <<'EOF'\n§ echo inside\nEOF\n§ ls",
			[]string{"cat <<'EOF'", "echo inside\nEOF", "ls"},
		},
		{"leading whitespace with §", "   § echo hello\n   § pwd", []string{"echo hello", "pwd"}},
		{"just § prefix", "§\necho hello\n§\npwd", []string{"echo hello", "pwd"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := ParseBlock(tt.block)
			var gotArgs []string
			for _, c := range cmds {
				gotArgs = append(gotArgs, c.Args)
			}
			if len(gotArgs) != len(tt.wantCmds) {
				t.Errorf("got %d commands %v, want %d %v", len(gotArgs), gotArgs, len(tt.wantCmds), tt.wantCmds)
				return
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.wantCmds[i] {
					t.Errorf("cmd[%d] = %q, want %q", i, gotArgs[i], tt.wantCmds[i])
				}
			}
		})
	}
}
