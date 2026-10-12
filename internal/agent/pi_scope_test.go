package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

func TestPiTakes(t *testing.T) {
	for _, c := range []struct {
		pattern, ref string
		want         bool
	}{
		{"xai/grok-4.6", "xai/grok-4.6", true},
		{"XAI/Grok-4.6", "xai/grok-4.6", true},
		{"xai/grok-4.6:high", "xai/grok-4.6", true},
		{"xai/*", "xai/grok-4.6", true},
		{"*grok*", "xai/grok-4.6", true},
		{"magpie/*", "magpie/deepseek/deepseek-v4", false}, // Pi's * stops at a /
		{"magpie/*/*", "magpie/deepseek/deepseek-v4", true},
		{"deepseek/*:low", "magpie/deepseek/deepseek-v4", true}, // the model id fits
		{"grok-4.6", "xai/grok-4.6", false},                     // a bare id: added beside
		{"xai/grok-4.6", "xai/grok-4.7", false},
		{"openai-codex/gpt-5.6-sol", "opencode-go/deepseek-v4-flash", false},
	} {
		if got := piTakes(c.pattern, c.ref); got != c.want {
			t.Errorf("piTakes(%q, %q) = %v", c.pattern, c.ref, got)
		}
	}
}

// Pi starts a new session on defaultModel only when enabledModels takes it
// in: the model magpie picks is added to the list, the user's entries
// kept; there is no list made where there was none, and none left empty
// (#160)
func TestPiModelJoinsEnabledModels(t *testing.T) {
	home := syncHome(t)
	settings := filepath.Join(home, ".pi", "agent", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)
	os.WriteFile(settings, []byte(`{
  // the user's own
  "enabledModels": ["xai/grok-4.6:high", "openai-codex/gpt-5.6-sol", "deepseek/*"],
  "defaultProvider": "xai",
  "defaultModel": "grok-4.6"
}
`), 0o644)
	f := pi(home).Field("model")
	if got := f.Get(); got != "xai/grok-4.6" {
		t.Fatalf("Get = %q", got)
	}
	if err := f.Set("opencode-go/deepseek-v4-flash-vision-exp"); err != nil {
		t.Fatal(err)
	}
	want := `["xai/grok-4.6:high","openai-codex/gpt-5.6-sol","deepseek/*","opencode-go/deepseek-v4-flash-vision-exp"]`
	if got := strings.Join(strings.Fields(mustGet(t, settings, "enabledModels")), ""); got != want {
		t.Errorf("enabledModels = %s, want %s", got, want)
	}
	if !strings.Contains(readFile(settings), "// the user's own") {
		t.Errorf("comment lost: %s", readFile(settings))
	}
	if got := f.Get(); got != "opencode-go/deepseek-v4-flash-vision-exp" {
		t.Errorf("Get = %q", got)
	}
	// a model the list takes in already isn't added again
	before := mustGet(t, settings, "enabledModels")
	for _, v := range []string{"openai-codex/gpt-5.6-sol", "deepseek/deepseek-v4-pro", "xai/grok-4.6"} {
		if err := f.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if got := mustGet(t, settings, "enabledModels"); got != before {
		t.Errorf("enabledModels grew: %s", got)
	}

	// set outside magpie: the model shown is the one Pi starts on
	edit.SetJSON(settings, edit.KV{Path: "defaultModel", Value: "grok-3"})
	if got := f.Get(); got != "xai/grok-4.6" {
		t.Errorf("Get with a default outside the list = %q, want the list's first", got)
	}

	// no list: none is made
	os.WriteFile(settings, []byte(`{"defaultProvider":"xai","defaultModel":"grok-4.6"}`), 0o644)
	if err := f.Set("opencode-go/deepseek-v4-flash"); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSON(settings, "enabledModels"); ok {
		t.Errorf("enabledModels made: %s", readFile(settings))
	}
	// an empty one means no list to Pi, and stays as it is
	os.WriteFile(settings, []byte(`{"enabledModels":[]}`), 0o644)
	if err := f.Set("opencode-go/deepseek-v4-flash"); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, settings, "enabledModels"); got != "[]" {
		t.Errorf("empty enabledModels = %s", got)
	}
}

// Turning Pi's model back to its own takes magpie's models out of the list
// with magpie's provider; a list left with nothing goes, not left empty
func TestPiModelOffLeavesEnabledModels(t *testing.T) {
	home := syncHome(t)
	settings := filepath.Join(home, ".pi", "agent", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)
	f := pi(home).Field("model")

	os.WriteFile(settings, []byte(`{"enabledModels":["xai/grok-4.6","magpie/*/*"]}`), 0o644)
	if err := f.Set("magpie/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("magpie/deepseek/deepseek-v4"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	// the user's glob is theirs
	if got := strings.Join(strings.Fields(mustGet(t, settings, "enabledModels")), ""); got != `["xai/grok-4.6","magpie/*/*"]` {
		t.Errorf("enabledModels = %s", got)
	}

	os.WriteFile(settings, []byte(`{"enabledModels":["magpie/relay/glm-4.6"],"defaultProvider":"magpie","defaultModel":"relay/glm-4.6"}`), 0o644)
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSON(settings, "enabledModels"); ok {
		t.Errorf("enabledModels left: %s", readFile(settings))
	}
}

func mustGet(t *testing.T, path, key string) string {
	t.Helper()
	v, ok := edit.GetJSON(path, key)
	if !ok {
		t.Fatalf("%s: no %s in %s", path, key, readFile(path))
	}
	return v
}

// A provider rename moves the scoped models too, including ones other than
// the default, while keeping the user's own models, patterns and comments.
func TestPiEnabledModelsRenamed(t *testing.T) {
	for _, makeAgent := range []func(string) *Agent{pi, omo, primeAgent} {
		t.Run(makeAgent(t.TempDir()).ID, func(t *testing.T) {
			home := syncHome(t)
			a := makeAgent(home)
			settings := a.Path
			writeFile(t, settings, `{
 // my models
 "enabledModels": ["xai/grok-4.6:high", "magpie/relay/glm-4.6", "magpie/relay/other:low", "magpie/relay/*", "magpie/relay-extra/keep"],
 "defaultProvider": "xai", "defaultModel": "grok-4.6"
}`)
			if err := a.Apply("model", "magpie/relay/glm-4.6"); err != nil {
				t.Fatal(err)
			}
			if _, err := RenameProvider("relay", "renamed"); err != nil {
				t.Fatal(err)
			}
			want := []string{"xai/grok-4.6:high", "magpie/renamed/glm-4.6", "magpie/renamed/other:low", "magpie/renamed/*", "magpie/relay-extra/keep"}
			if got := piScope(settings); !slices.Equal(got, want) {
				t.Fatalf("scope after rename = %q, want %q", got, want)
			}
			if got := a.Field("model").Get(); got != "magpie/renamed/glm-4.6" {
				t.Fatalf("model after rename = %q", got)
			}
			if !strings.Contains(readFile(settings), "// my models") {
				t.Fatal("user comment lost")
			}
			if err := a.Disconnect(); err != nil {
				t.Fatal(err)
			}
			want = []string{"xai/grok-4.6:high", "magpie/renamed/*"}
			if got := piScope(settings); !slices.Equal(got, want) {
				t.Fatalf("scope after disconnect = %q, want %q", got, want)
			}
		})
	}
}

// Scoped references follow a rename even when Pi starts on its own model.
// An unrelated, missing or empty scope is not rewritten.
func TestPiEnabledModelsRenameWithOwnDefault(t *testing.T) {
	for _, c := range []struct {
		name, scope string
		want        []string
	}{
		{"native default", `,"enabledModels":["xai/grok-4.6","magpie/relay/glm-4.6:high"]`, []string{"xai/grok-4.6", "magpie/renamed/glm-4.6:high"}},
		{"unrelated", `,"enabledModels":["xai/grok-4.6","xai/grok-4.6","magpie/relay-extra/keep"]`, nil},
		{"missing", "", nil},
		{"empty", `,"enabledModels":[]`, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := syncHome(t)
			a := pi(home)
			before := "{ // my defaults\n \"defaultProvider\":\"xai\",\"defaultModel\":\"grok-4.6\"" + c.scope + "}\n"
			writeFile(t, a.Path, before)
			if _, err := RenameProvider("relay", "renamed"); err != nil {
				t.Fatal(err)
			}
			if c.want == nil {
				if got := readFile(a.Path); got != before {
					t.Fatalf("unrelated settings changed:\n%s", got)
				}
			} else if got := piScope(a.Path); !slices.Equal(got, c.want) {
				t.Fatalf("scope = %q, want %q", got, c.want)
			}
			if got := a.Field("model").Get(); got != "xai/grok-4.6" {
				t.Fatalf("native default changed to %q", got)
			}
		})
	}
}
