package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (l *Launcher) command(app App) (Command, error) {
	c := Command{Env: append([]string(nil), l.env...), Dir: l.cfg.HomeDir}
	switch app {
	case Browser:
		if l.cfg.Browser == Chromium {
			if err := l.prepareChromium(); err != nil {
				return Command{}, fmt.Errorf("%w: chromium profile", ErrUnavailable)
			}
			c.Path = "/usr/bin/chromium"
			extension := filepath.Join(l.cfg.HomeDir, ".config", "sidekicks", "newtab")
			c.Args = []string{"--no-first-run", "--no-default-browser-check", "--password-store=basic", "--user-data-dir=" + l.cfg.ChromiumProfile, "--class=SidekicksBrowser", "--window-size=1120,548", "--window-position=80,56", "--disable-extensions-except=" + extension, "--load-extension=" + extension, "--new-window", l.cfg.HomeURL}
			if l.cfg.ChromiumNoSandbox {
				c.Args = append([]string{"--no-sandbox"}, c.Args...)
			}
		} else {
			if err := os.MkdirAll(l.cfg.FirefoxProfile, 0700); err != nil {
				return Command{}, fmt.Errorf("%w: firefox profile", ErrUnavailable)
			}
			home, _ := json.Marshal(l.cfg.HomeURL)
			prefs := []byte("user_pref(\"browser.startup.homepage\", " + string(home) + ");\nuser_pref(\"browser.startup.page\", 1);\nuser_pref(\"browser.tabs.inTitlebar\", 0);\n")
			if err := atomicWrite(filepath.Join(l.cfg.FirefoxProfile, "user.js"), prefs); err != nil {
				return Command{}, fmt.Errorf("%w: firefox preferences", ErrUnavailable)
			}
			c.Path = "/usr/bin/firefox-esr"
			c.Args = []string{"--profile", l.cfg.FirefoxProfile, "--class", "SidekicksBrowser", "--width", "1120", "--height", "548", "--new-window", l.cfg.HomeURL}
		}
	case Terminal:
		c.Path = "/usr/bin/xfce4-terminal"
		// --role is supported by both installed 1.0.4 and 1.1.4. Newer --class is
		// deliberately avoided; EWMH matching requires class+this exact role.
		c.Args = []string{"--disable-server", "--role=SidekicksTerminal", "--title=Sidekicks Terminal", "--geometry=88x24", "--font=DejaVu Sans Mono 11", "--color-bg=#10263d", "--color-text=#e8f7ff", "--show-borders", "--hide-toolbar", "--hide-scrollbar", "--working-directory=" + l.cfg.HomeDir}
	case Files:
		c.Path = "/usr/bin/thunar"
		c.Args = []string{l.cfg.HomeDir}
	default:
		return Command{}, ErrInvalid
	}
	return c, nil
}
func (l *Launcher) prepareChromium() error {
	dir := filepath.Join(l.cfg.ChromiumProfile, "Default")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	name := filepath.Join(dir, "Preferences")
	var data []byte
	f, err := os.Open(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
			f.Close()
			return errors.New("preferences exceeds bounds")
		}
		data, err = io.ReadAll(io.LimitReader(f, (4<<20)+1))
		f.Close()
		if err != nil || len(data) > 4<<20 {
			return errors.New("preferences exceeds bounds")
		}
	}
	prefs := map[string]any{}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &prefs); err != nil || prefs == nil {
			return errors.New("invalid preferences object")
		}
	}
	section := func(key string) (map[string]any, error) {
		if v, exists := prefs[key]; exists {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("invalid preferences section")
			}
			return m, nil
		}
		m := map[string]any{}
		prefs[key] = m
		return m, nil
	}
	browser, err := section("browser")
	if err != nil {
		return err
	}
	session, err := section("session")
	if err != nil {
		return err
	}
	browser["custom_chrome_frame"] = true
	browser["show_home_button"] = true
	prefs["homepage"] = l.cfg.HomeURL
	prefs["homepage_is_newtabpage"] = false
	session["restore_on_startup"] = 4
	session["startup_urls"] = []string{l.cfg.HomeURL}
	data, err = json.Marshal(prefs)
	if err != nil {
		return err
	}
	if err = atomicWrite(name, data); err != nil {
		return err
	}
	extension := filepath.Join(l.cfg.HomeDir, ".config", "sidekicks", "newtab")
	if err = os.MkdirAll(extension, 0700); err != nil {
		return err
	}
	home, _ := json.Marshal(l.cfg.HomeURL)
	files := map[string][]byte{
		"manifest.json": []byte(`{"manifest_version":3,"name":"Sidekicks Home","version":"1.0.0","chrome_url_overrides":{"newtab":"newtab.html"}}`),
		"newtab.html":   []byte(`<!doctype html><meta charset="utf-8"><title>Sidekicks Home</title><script src="newtab.js"></script>`),
		"newtab.js":     []byte("location.replace(" + string(home) + ");\n"),
	}
	for n, b := range files {
		if err = atomicWrite(filepath.Join(extension, n), b); err != nil {
			return err
		}
	}
	return nil
}
func atomicWrite(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".sidekicks-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}
