// Package session prepares and supervises the Linux guest desktop session.
package session

import (
	"bytes"
	"embed"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*
var templates embed.FS

// DefaultAssets is the default location of desktop session assets.
const DefaultAssets = "/usr/share/sidekicks/desktop"

// DefaultHome is the default guest home directory.
const DefaultHome = "/tmp/sidekicks-home"

// Config configures the desktop session and its runtime dependencies.
type Config struct {
	Home          string
	Assets        string
	Display       string
	Monitor       string
	HomeURL       string
	TerminalClass string
	// Runner is injectable for tests; nil uses owned Linux process groups.
	Runner Runner
}

func defaults(cfg Config) (Config, error) {
	if cfg.Home == "" {
		cfg.Home = DefaultHome
	}
	if cfg.Assets == "" {
		cfg.Assets = DefaultAssets
	}
	if cfg.Display == "" {
		cfg.Display = ":99"
	}
	if cfg.Monitor == "" {
		cfg.Monitor = "screen"
	}
	if cfg.HomeURL == "" {
		cfg.HomeURL = "http://127.0.0.1:8081/home"
	}
	if cfg.TerminalClass == "" {
		cfg.TerminalClass = "Xfce4-terminal"
	}
	if !filepath.IsAbs(cfg.Home) || filepath.Clean(cfg.Home) != cfg.Home || cfg.Home == "/" || !filepath.IsAbs(cfg.Assets) || filepath.Clean(cfg.Assets) != cfg.Assets || cfg.Assets == "/" {
		return cfg, errors.New("home and assets must be distinct clean absolute guest paths")
	}
	if cfg.Home == cfg.Assets || strings.HasPrefix(cfg.Home, cfg.Assets+"/") {
		return cfg, errors.New("home must not overwrite desktop assets")
	}
	if cfg.Display != ":99" {
		return cfg, errors.New("only private display :99 is supported")
	}
	for _, value := range []string{cfg.Home, cfg.Assets, cfg.Monitor, cfg.TerminalClass, cfg.HomeURL} {
		if strings.ContainsAny(value, "\x00\r\n\t") {
			return cfg, errors.New("invalid desktop configuration value")
		}
	}
	if cfg.Monitor == "" || strings.ContainsAny(cfg.Monitor, "/\\<>\"' ") {
		return cfg, errors.New("invalid monitor name")
	}
	if cfg.TerminalClass != "Xfce4-terminal" && cfg.TerminalClass != "SidekicksTerminal" {
		return cfg, errors.New("unsupported managed terminal class")
	}
	u, err := url.Parse(cfg.HomeURL)
	if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:8081" || u.Path != "/home" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return cfg, errors.New("home must use the fixed private desktop page")
	}
	return cfg, nil
}

type value struct {
	Type  string `xml:"type,attr"`
	Value string `xml:"value,attr"`
}
type property struct {
	Name       string     `xml:"name,attr"`
	Type       string     `xml:"type,attr"`
	Value      string     `xml:"value,attr,omitempty"`
	Properties []property `xml:"property"`
	Values     []value    `xml:"value"`
}
type channel struct {
	XMLName    xml.Name   `xml:"channel"`
	Name       string     `xml:"name,attr"`
	Version    string     `xml:"version,attr"`
	Unlocked   string     `xml:"unlocked,attr,omitempty"`
	Properties []property `xml:"property"`
}

func stringProperty(name, typ, v string) property {
	return property{Name: name, Type: typ, Value: v}
}
func emptyProperty(name string, children ...property) property {
	return property{Name: name, Type: "empty", Properties: children}
}
func arrayProperty(name string, values ...string) property {
	p := property{Name: name, Type: "array"}
	for _, v := range values {
		p.Values = append(p.Values, value{Type: "string", Value: v})
	}
	return p
}

// Generate renders the original canonical templates and fixed launchers into a
// fresh private home. It never writes global /etc, executes Python or copies any
// host settings. Asset files must be supplied by the read-only image filesystem.
func Generate(config Config) error {
	cfg, err := defaults(config)
	if err != nil {
		return err
	}
	for _, name := range []string{"wallpaper.png", "icons/browser.png", "icons/terminal.png", "icons/files.png", "polish/settings.ini", "polish/gtk.css", "polish/icons/index.theme", "polish/icons/scalable/places/folder.svg"} {
		st, err := os.Stat(filepath.Join(cfg.Assets, name))
		if err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("required desktop asset is absent: %s", name)
		}
	}
	if err = os.Mkdir(cfg.Home, 0700); err != nil {
		return fmt.Errorf("refusing to create desktop home: %w", err)
	}
	if err = os.MkdirAll(filepath.Join(cfg.Home, ".run"), 0700); err != nil {
		return err
	}
	channels := filepath.Join(cfg.Home, ".config/xfce4/xfconf/xfce-perchannel-xml")
	for _, name := range []string{"xsettings", "xfwm4", "xfce4-session", "xfce4-desktop"} {
		raw, err := templates.ReadFile("templates/" + name + ".xml")
		if err != nil {
			return err
		}
		var c channel
		if err = xml.Unmarshal(raw, &c); err != nil {
			return err
		}
		if name == "xfce4-desktop" {
			for i := range c.Properties {
				if c.Properties[i].Name == "backdrop" {
					c.Properties[i] = emptyProperty("backdrop", emptyProperty("screen0", monitorProperty("monitor0", cfg.Assets), monitorProperty("monitor"+cfg.Monitor, cfg.Assets)))
				}
			}
		}
		if name == "xfce4-session" {
			c.Properties = append(c.Properties, sessionClients())
			for i := range c.Properties {
				if c.Properties[i].Name == "general" {
					c.Properties[i].Properties = append(c.Properties[i].Properties, stringProperty("FailsafeSessionName", "string", "Failsafe"))
				}
			}
		}
		if err = writeChannel(filepath.Join(channels, name+".xml"), c); err != nil {
			return err
		}
	}
	raw, err := templates.ReadFile("templates/xfce4-panel.xml")
	if err != nil {
		return err
	}
	// The named-user lock is loaded from a private system configuration layer.
	// It is UX curation, not a security boundary for the guest user.
	if err = write(filepath.Join(cfg.Home, ".config-system/xfce4/xfconf/xfce-perchannel-xml/xfce4-panel.xml"), raw); err != nil {
		return err
	}
	for _, entry := range []struct{ src, dst string }{{"templates/terminalrc", ".config/xfce4/terminal/terminalrc"}, {"templates/xfce-applications.menu", ".config/menus/xfce-applications.menu"}} {
		data, err := templates.ReadFile(entry.src)
		if err != nil {
			return err
		}
		if err = write(filepath.Join(cfg.Home, entry.dst), data); err != nil {
			return err
		}
	}
	for _, entry := range []struct{ src, dst string }{{"polish/settings.ini", ".config/gtk-3.0/settings.ini"}, {"polish/gtk.css", ".config/gtk-3.0/gtk.css"}, {"polish/icons/index.theme", ".local/share/icons/Sidekicks/index.theme"}, {"polish/icons/scalable/places/folder.svg", ".local/share/icons/Sidekicks/scalable/places/folder.svg"}} {
		data, err := os.ReadFile(filepath.Join(cfg.Assets, entry.src))
		if err != nil {
			return err
		}
		if err = write(filepath.Join(cfg.Home, entry.dst), data); err != nil {
			return err
		}
	}
	for i, app := range []string{"browser", "terminal", "files"} {
		class := map[string]string{"browser": "SidekicksBrowser", "terminal": cfg.TerminalClass, "files": "Thunar"}[app]
		launcher := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nComment=Open or restore the Sidekicks %s\nExec=/usr/local/bin/sidekicks-agent launch %s\nIcon=%s\nTerminal=false\nStartupWMClass=%s\n", strings.ToUpper(app[:1])+app[1:], app, app, filepath.Join(cfg.Assets, "icons", app+".png"), class)
		filename := "sidekicks-" + app + ".desktop"
		for _, dir := range []string{".local/share/applications", fmt.Sprintf(".config/xfce4/panel/launcher-%d", i+1)} {
			if err = write(filepath.Join(cfg.Home, dir, filename), []byte(launcher)); err != nil {
				return err
			}
		}
	}
	if err = writeChannel(filepath.Join(channels, "xfce4-keyboard-shortcuts.xml"), shortcuts()); err != nil {
		return err
	}
	if err = write(filepath.Join(cfg.Home, ".config/gtk-3.0/bookmarks"), []byte("file://"+cfg.Home+"/Sidekicks Sidekicks\n")); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(cfg.Home, "Sidekicks"), 0700); err != nil {
		return err
	}
	for _, name := range []string{"picom", "tint2", "openbox", "xfsettingsd", "pulseaudio"} {
		if err = write(filepath.Join(cfg.Home, ".config/autostart", name+".desktop"), []byte("[Desktop Entry]\nType=Application\nName=Disabled duplicate "+name+" autostart\nHidden=true\n")); err != nil {
			return err
		}
	}
	return write(filepath.Join(cfg.Home, ".sidekicks-session-configured"), []byte("sidekicks-xfce-v1\n"))
}
func monitorProperty(name, assets string) property {
	return emptyProperty(name, emptyProperty("workspace0", stringProperty("last-image", "string", filepath.Join(assets, "wallpaper.png")), stringProperty("image-style", "int", "5"), stringProperty("color-style", "int", "0")))
}
func sessionClients() property {
	clients := [][]string{{"xfwm4"}, {"xfsettingsd", "--no-daemon"}, {"xfce4-panel"}, {"Thunar", "--daemon"}, {"xfdesktop"}}
	failsafe := emptyProperty("Failsafe", stringProperty("IsFailsafe", "bool", "true"), stringProperty("Count", "int", "5"))
	for i, args := range clients {
		failsafe.Properties = append(failsafe.Properties, arrayProperty(fmt.Sprintf("Client%d_Command", i), args...), stringProperty(fmt.Sprintf("Client%d_Priority", i), "int", fmt.Sprint(15+i*5)), stringProperty(fmt.Sprintf("Client%d_PerScreen", i), "bool", "false"))
	}
	return emptyProperty("sessions", failsafe)
}
func shortcuts() channel {
	command := emptyProperty("custom", stringProperty("override", "bool", "true"))
	for _, entry := range [][2]string{{"<Super>b", "browser"}, {"<Super>Return", "terminal"}, {"<Super>e", "files"}} {
		command.Properties = append(command.Properties, stringProperty(entry[0], "string", "/usr/local/bin/sidekicks-agent launch "+entry[1]))
	}
	command.Properties = append(command.Properties, stringProperty("<Primary>Escape", "string", "xfdesktop --menu"))
	wm := emptyProperty("custom", stringProperty("override", "bool", "true"))
	for _, entry := range [][2]string{{"<Super>d", "show_desktop_key"}, {"<Alt>F4", "close_window_key"}, {"<Alt>F9", "hide_window_key"}, {"<Alt>F10", "maximize_window_key"}, {"<Alt>Tab", "cycle_windows_key"}, {"<Alt><Shift>Tab", "cycle_reverse_windows_key"}, {"<Alt>F12", "fullscreen_key"}, {"<Alt>F7", "move_window_key"}, {"<Alt>F8", "resize_window_key"}, {"<Alt>space", "popup_menu_key"}, {"Escape", "cancel_key"}, {"Left", "left_key"}, {"Right", "right_key"}, {"Up", "up_key"}, {"Down", "down_key"}} {
		wm.Properties = append(wm.Properties, stringProperty(entry[0], "string", entry[1]))
	}
	return channel{Name: "xfce4-keyboard-shortcuts", Version: "1.0", Properties: []property{emptyProperty("commands", command), emptyProperty("xfwm4", wm)}}
}
func writeChannel(name string, c channel) error {
	raw, err := xml.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return write(name, append([]byte(xml.Header), append(raw, '\n')...))
}
func write(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	return os.WriteFile(name, bytes.Clone(data), 0600)
}
