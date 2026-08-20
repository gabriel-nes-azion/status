package main

import "testing"

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want options
	}{
		{"none", nil, options{}},
		{"conf separate", []string{"--conf", "/etc/status.toml"}, options{confPath: "/etc/status.toml"}},
		{"conf equals", []string{"--conf=/etc/status.toml"}, options{confPath: "/etc/status.toml"}},
		{"config alias", []string{"--config", "a.toml"}, options{confPath: "a.toml"}},
		{"config equals alias", []string{"--config=a.toml"}, options{confPath: "a.toml"}},
		{"short", []string{"-c", "a.toml"}, options{confPath: "a.toml"}},
		{"no save", []string{"--no-save"}, options{noSave: true}},
		{"both", []string{"--no-save", "-c", "a.toml"}, options{confPath: "a.toml", noSave: true}},
		// A path that looks like a flag is still a path: --conf takes the next
		// argument whatever it is.
		{"flaggy path", []string{"--conf", "--weird.toml"}, options{confPath: "--weird.toml"}},
		{"last wins", []string{"-c", "a.toml", "-c", "b.toml"}, options{confPath: "b.toml"}},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: parseArgs(%q) = %+v, want %+v", c.name, c.args, got, c.want)
		}
	}
}

func TestParseArgsRejections(t *testing.T) {
	for _, args := range [][]string{
		{"--conf"},               // missing value
		{"--config"},             //
		{"-c"},                   //
		{"--conf="},              // empty value
		{"--config="},            //
		{"nonsense"},             // unknown positional
		{"--nope"},               // unknown flag
		{"-c", "a.toml", "junk"}, // trailing garbage
	} {
		if got, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%q) = %+v, want an error", args, got)
		}
	}
}
