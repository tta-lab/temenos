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
		// Regression: heredoc body lines starting with prefix must not be re-parsed as commands.
		{
			"heredoc body with prefix not re-parsed",
			"§ cat <<'EOF'\n§ echo inside\nEOF\n§ ls",
			[]string{"cat <<'EOF'\n§ echo inside\nEOF", "ls"},
		},
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

func TestParseHeredocSpec(t *testing.T) {
	tests := []struct {
		args       string
		wantDelim  string
		wantIsDash bool
		wantOK     bool
	}{
		{"cat <<EOF", "EOF", false, true},
		{"cat <<'EOF'", "EOF", false, true},
		{"cat <<\"EOF\"", "EOF", false, true},
		{"cat <<-EOF", "EOF", true, true},
		{"cat <<-'MARKER'", "MARKER", true, true},
		{"cat <<- 'PLANEOF'", "PLANEOF", true, true},
		{"cat <<-\"PLANEOF\"", "PLANEOF", true, true},
		{"cat <<'EOF' | wc -l", "EOF", false, true},
		{"cat <<'EOF' > out.txt", "EOF", false, true},
		{"ls -la", "", false, false},
		{"echo hello", "", false, false},
		{"cat <<", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			spec, ok := parseHeredocSpec(tt.args)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if spec.delim != tt.wantDelim {
				t.Errorf("delim = %q, want %q", spec.delim, tt.wantDelim)
			}
			if ok && spec.isDash != tt.wantIsDash {
				t.Errorf("isDash = %v, want %v", spec.isDash, tt.wantIsDash)
			}
		})
	}
}

func TestIsHeredocClose(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		delim  string
		isDash bool
		want   bool
	}{
		// Non-dash (<<): exact match only
		{"exact match", "EOF", "EOF", false, true},
		{"trailing space non-dash", "EOF ", "EOF", false, false},
		{"leading space non-dash", " EOF", "EOF", false, false},
		{"wrong delimiter", "NOTEOF", "EOF", false, false},
		{"empty line", "", "EOF", false, false},
		{"partial match", "EO", "EOF", false, false},
		// Dash (<<-): leading tabs stripped
		{"tab-indented dash", "\t\tEOF", "EOF", true, true},
		{"exact match dash", "EOF", "EOF", true, true},
		{"space-padded dash does not match", "  EOF  ", "EOF", true, false},
		{"trailing space dash", "EOF  ", "EOF", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := heredocSpec{delim: tt.delim, isDash: tt.isDash}
			if got := isHeredocClose(tt.line, spec); got != tt.want {
				t.Errorf("isHeredocClose(%q, {%q, isDash=%v}) = %v, want %v",
					tt.line, tt.delim, tt.isDash, got, tt.want)
			}
		})
	}
}
