package session

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	base := t.TempDir()
	assets := filepath.Join(base, "assets")
	for _, name := range []string{"wallpaper.png", "icons/browser.png", "icons/terminal.png", "icons/files.png", "polish/settings.ini", "polish/gtk.css", "polish/icons/index.theme", "polish/icons/scalable/places/folder.svg"} {
		target := filepath.Join(assets, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("asset:"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return Config{Home: filepath.Join(base, "home"), Assets: assets}
}
func loadChannel(t *testing.T, name string) channel {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var c channel
	if err = xml.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func flatten(c channel) map[string]property {
	out := map[string]property{}
	var visit func(string, []property)
	visit = func(prefix string, properties []property) {
		for _, p := range properties {
			out[prefix+"/"+p.Name] = p
			visit(prefix+"/"+p.Name, p.Properties)
		}
	}
	visit("", c.Properties)
	return out
}

// TestCanonicalCopiesUnmodified verifies canonical copies unmodified.
func TestCanonicalCopiesUnmodified(t *testing.T) {
	entries, err := templates.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		embedded, _ := templates.ReadFile("templates/" + entry.Name())
		source, err := os.ReadFile(filepath.Join("../../../../infrastructure/images/assets/desktop/polish/xfce/templates", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(embedded, source) {
			t.Fatalf("canonical template changed: %s", entry.Name())
		}
	}
}

// TestGenerateCuratedSession verifies generate curated session.
func TestGenerateCuratedSession(t *testing.T) {
	cfg := testConfig(t)
	cfg.Monitor = "DUMMY0"
	cfg.TerminalClass = "SidekicksTerminal"
	if err := Generate(cfg); err != nil {
		t.Fatal(err)
	}
	channels := filepath.Join(cfg.Home, ".config/xfce4/xfconf/xfce-perchannel-xml")
	session := flatten(loadChannel(t, filepath.Join(channels, "xfce4-session.xml")))
	if session["/general/PromptOnLogout"].Value != "true" || session["/general/SaveOnExit"].Value != "false" {
		t.Fatal("logout/session preservation policy changed")
	}
	settingsCommand := session["/sessions/Failsafe/Client1_Command"].Values
	if len(settingsCommand) != 2 || settingsCommand[1].Value != "--no-daemon" {
		t.Fatal("settings process is not owned by session tree")
	}
	desktop := flatten(loadChannel(t, filepath.Join(channels, "xfce4-desktop.xml")))
	for _, monitor := range []string{"monitor0", "monitorDUMMY0"} {
		if desktop["/backdrop/screen0/"+monitor+"/workspace0/last-image"].Value != filepath.Join(cfg.Assets, "wallpaper.png") {
			t.Fatal("wrong wallpaper/monitor")
		}
	}
	wm := flatten(loadChannel(t, filepath.Join(channels, "xfwm4.xml")))
	if wm["/general/box_move"].Value != "false" || wm["/general/box_resize"].Value != "false" {
		t.Fatal("opaque window movement changed")
	}
	panel := loadChannel(t, filepath.Join(cfg.Home, ".config-system/xfce4/xfconf/xfce-perchannel-xml/xfce4-panel.xml"))
	if panel.Unlocked != "root" {
		t.Fatal("named-user panel lock missing")
	}
	if count := len(flatten(panel)["/panels/panel-1/plugin-ids"].Values); count != 3 {
		t.Fatal("expected exactly three launcher plugins", count)
	}
	for index, app := range []string{"browser", "terminal", "files"} {
		filename := "sidekicks-" + app + ".desktop"
		raw, err := os.ReadFile(filepath.Join(cfg.Home, ".config/xfce4/panel/launcher-"+string(rune('1'+index)), filename))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "Exec=/usr/local/bin/sidekicks-agent launch "+app+"\n") {
			t.Fatal("launcher invokes non-fixed command")
		}
		if app == "terminal" && !strings.Contains(string(raw), "StartupWMClass=SidekicksTerminal\n") {
			t.Fatal("terminal identity lost")
		}
	}
	shortcuts := flatten(loadChannel(t, filepath.Join(channels, "xfce4-keyboard-shortcuts.xml")))
	if shortcuts["/commands/custom/<Super>Return"].Value != "/usr/local/bin/sidekicks-agent launch terminal" || shortcuts["/xfwm4/custom/<Alt>F12"].Value != "fullscreen_key" {
		t.Fatal("normal window shortcuts missing")
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, "Sidekicks")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openbox", "tint2", "picom", "xfsettingsd", "pulseaudio"} {
		raw, err := os.ReadFile(filepath.Join(cfg.Home, ".config/autostart", name+".desktop"))
		if err != nil || !strings.Contains(string(raw), "Hidden=true") {
			t.Fatal("duplicate desktop autostart not suppressed", name, err)
		}
	}
}

// TestGenerateRejectsMissingAssetsOrExistingHome verifies generate rejects missing assets or existing home.
func TestGenerateRejectsMissingAssetsOrExistingHome(t *testing.T) {
	cfg := testConfig(t)
	if err := os.Remove(filepath.Join(cfg.Assets, "icons/browser.png")); err != nil {
		t.Fatal(err)
	}
	if err := Generate(cfg); err == nil {
		t.Fatal("missing asset accepted")
	}
	cfg = testConfig(t)
	if err := os.MkdirAll(cfg.Home, 0700); err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(cfg.Home, "unsaved-session")
	if err := os.WriteFile(proof, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Generate(cfg); err == nil {
		t.Fatal("populated home overwritten")
	}
	raw, _ := os.ReadFile(proof)
	if string(raw) != "retain" {
		t.Fatal("existing state changed")
	}
	cfg = testConfig(t)
	if err := Generate(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Generate(cfg); err == nil {
		t.Fatal("configured desktop silently overwritten")
	}
}

// TestConfigValidation verifies config validation.
func TestConfigValidation(t *testing.T) {
	for _, edit := range []func(*Config){func(c *Config) {
		c.Home = "/"
	}, func(c *Config) {
		c.Home = "relative"
	}, func(c *Config) {
		c.Assets = "/"
	}, func(c *Config) {
		c.Home = c.Assets
	}, func(c *Config) {
		c.Display = ":0"
	}, func(c *Config) {
		c.Monitor = "../screen"
	}, func(c *Config) {
		c.Monitor = "foo\n"
	}, func(c *Config) {
		c.TerminalClass = "Terminal"
	}, func(c *Config) {
		c.HomeURL = "https://example.com"
	}} {
		cfg := testConfig(t)
		edit(&cfg)
		if _, err := defaults(cfg); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	cfg, err := defaults(Config{})
	if err != nil || cfg.Home != DefaultHome || cfg.Assets != DefaultAssets {
		t.Fatal("wrong defaults", cfg, err)
	}
}
