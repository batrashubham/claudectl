package machine

import (
	"testing"

	"github.com/batrashubham/claudectl/internal/config"
)

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Shubhams-MacBook-Pro.local": "shubhams-macbook-pro",
		"dev box_01":                 "dev-box-01",
		"...":                        "machine",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if ValidateName("Bad Name") == nil || ValidateName("good-1") != nil {
		t.Error("ValidateName accepts only slugs")
	}
}

func TestLocalName_PersistsDerivedName(t *testing.T) {
	t.Setenv("CLAUDECTL_HOME", t.TempDir())
	first, err := LocalName("")
	if err != nil || first == "" {
		t.Fatalf("LocalName = %q, %v", first, err)
	}
	if err := SetLocalName("renamed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := LocalName(""); got != "renamed" {
		t.Errorf("persisted name not used: %q", got)
	}
	if got, _ := LocalName("configured"); got != "configured" {
		t.Errorf("configured name should win: %q", got)
	}
}

func TestMapPath(t *testing.T) {
	rules := []config.PathMap{
		{From: "/Users/me", To: "/home/me"},
		{From: "/Users/me/work", To: "/srv/work"},
	}
	cases := []struct{ in, fromHome, toHome, want string }{
		{"/Users/me/work/api", "", "", "/srv/work/api"}, // longest rule wins
		{"/Users/me/play", "", "", "/home/me/play"},
		{"/Users/meow/x", "", "", "/Users/meow/x"}, // prefix must end at a path boundary
		{"/opt/x", "", "", "/opt/x"},
	}
	for _, c := range cases {
		if got := MapPath(c.in, rules, c.fromHome, c.toHome); got != c.want {
			t.Errorf("MapPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := MapPath("/Users/a/code/x", nil, "/Users/a", "/home/a"); got != "/home/a/code/x" {
		t.Errorf("home swap = %q", got)
	}
}

func TestManifest_WriteOnlyOnChangeAndList(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{Name: "laptop", OS: "darwin", Home: "/Users/me"}
	if changed, err := WriteManifest(dir, m); err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	if changed, _ := WriteManifest(dir, m); changed {
		t.Error("identical manifest should not count as a change")
	}
	got, ok := Find(dir, "laptop")
	if !ok || got.Home != "/Users/me" {
		t.Errorf("Find = %+v, %v", got, ok)
	}
}
