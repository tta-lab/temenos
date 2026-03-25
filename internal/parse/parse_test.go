package parse

import "testing"

func TestParseBlock(t *testing.T) {
	prefix := "§ "
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
		{"non-prefixed lines ignored", "some text\n§ pwd\nmore text\n§ ls\n", []string{"pwd", "ls"}},
		{"heredoc with dash", "\n§ cat <<-EOF\n\tindented\nEOF\n", []string{"cat <<-EOF\n\tindented\nEOF"}},
		{"unclosed heredoc", "\n§ cat <<'EOF'\nline1\nline2", []string{"cat <<'EOF'\nline1\nline2"}},
		{"prefix only no args", "\n§ \n§ ls\n", []string{"ls"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := ParseBlock(tt.block, prefix)
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

func TestParseBlock_CustomPrefix(t *testing.T) {
	block := "! run ls\n! run pwd\nsome text"
	cmds := ParseBlock(block, "! run ")
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2", len(cmds))
	}
	if cmds[0].Args != "ls" {
		t.Errorf("cmd[0].Args = %q, want %q", cmds[0].Args, "ls")
	}
	if cmds[1].Args != "pwd" {
		t.Errorf("cmd[1].Args = %q, want %q", cmds[1].Args, "pwd")
	}
}

func TestHeredocDelimiter(t *testing.T) {
	tests := []struct {
		args      string
		wantDelim string
		wantOK    bool
	}{
		{"cat <<EOF", "EOF", true},
		{"cat <<'EOF'", "EOF", true},
		{"cat <<\"EOF\"", "EOF", true},
		{"cat <<-EOF", "EOF", true},
		{"cat <<-'MARKER'", "MARKER", true},
		{"cat <<- 'PLANEOF'", "PLANEOF", true},
		{"cat <<-\"PLANEOF\"", "PLANEOF", true},
		{"cat <<'EOF' | wc -l", "EOF", true},
		{"cat <<'EOF' > out.txt", "EOF", true},
		{"ls -la", "", false},
		{"echo hello", "", false},
		{"cat <<", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			delim, ok := heredocDelimiter(tt.args)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if delim != tt.wantDelim {
				t.Errorf("delim = %q, want %q", delim, tt.wantDelim)
			}
		})
	}
}

func TestIsHeredocClose(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		delim string
		want  bool
	}{
		{"exact match", "EOF", "EOF", true},
		{"with surrounding whitespace", "  EOF  ", "EOF", true},
		{"tab-indented", "\t\tEOF", "EOF", true},
		{"wrong delimiter", "NOTEOF", "EOF", false},
		{"empty line", "", "EOF", false},
		{"partial match", "EO", "EOF", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHeredocClose(tt.line, tt.delim); got != tt.want {
				t.Errorf("isHeredocClose(%q, %q) = %v, want %v", tt.line, tt.delim, got, tt.want)
			}
		})
	}
}
