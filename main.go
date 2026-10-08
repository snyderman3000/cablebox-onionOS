// Cablebox: browse TV shows by channel and order them into Sonarr, for OnionOS on the Miyoo Mini Plus.
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const version = "0.1.3"
const userAgent = "Cablebox/" + version + " (OnionOS TV guide)"

// Config lives in App/Cablebox/data/config.json.
type Config struct {
	TMDBKey      string   `json:"tmdb_api_key"`
	OMDbKey      string   `json:"omdb_api_key"`
	Region       string   `json:"region"`
	Language     string   `json:"language"`
	Services     []string `json:"services"` // streaming services that get their own channel, by name
	RefreshHours int      `json:"refresh_hours"`

	SonarrURL     string `json:"sonarr_url"`
	SonarrKey     string `json:"sonarr_api_key"`
	SonarrProfile string `json:"sonarr_quality_profile"`  // name or id; blank = first
	SonarrRoot    string `json:"sonarr_root_folder"`      // blank = first
	SonarrLang    string `json:"sonarr_language_profile"` // Sonarr v3 only; blank = first
	SonarrSearch  *bool  `json:"sonarr_search,omitempty"` // search as soon as something is ordered (default true)

	Problem  string `json:"-"`
	Borrowed bool   `json:"-"` // keys came from Rewind's config
}

var defaultServices = []string{"Netflix", "Hulu", "Prime Video", "Disney+", "HBO Max", "Apple TV", "Paramount+", "Peacock"}

func defaultConfig() Config {
	return Config{Region: "US", Language: "en-US", RefreshHours: 6, Services: defaultServices}
}

func (c Config) search() bool { return c.SonarrSearch == nil || *c.SonarrSearch }

type Env struct {
	AppDir  string
	DataDir string
	Config  Config
	HTTP    *http.Client
	// overridable for tests
	TMDBBase  string
	OMDbBase  string
	ImageBase string
	Now       func() time.Time
}

func newEnv() *Env {
	exe, _ := os.Executable()
	appDir := filepath.Dir(exe)
	env := &Env{AppDir: appDir, DataDir: filepath.Join(appDir, "data"), Now: time.Now}
	if d := os.Getenv("CABLEBOX_DATA"); d != "" {
		env.DataDir = d
	}
	os.MkdirAll(env.DataDir, 0o755)
	env.Config = loadConfig(filepath.Join(env.DataDir, "config.json"), filepath.Join(appDir, "..", "Rewind", "data", "config.json"))
	tr := newTransport()
	env.HTTP = &http.Client{Transport: tr, Timeout: 30 * time.Second}
	return env
}

// newTransport makes plain HTTP/1.1 connections: one slow answer (a poster, a
// big details page) can't hold up everything else the way it can on a single
// shared HTTP/2 connection. HTTP/2 has to be left out of the TLS handshake
// (ALPN) too, or servers answer in HTTP/2 anyway.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{NextProtos: []string{"http/1.1"}},
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   15 * time.Second,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       4,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// loadConfig reads config.json, filling in defaults; TMDB and OMDb keys are
// borrowed from Rewind's config when Cablebox has none of its own.
func loadConfig(path, rewindPath string) Config {
	cfg := defaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		starter := map[string]any{"tmdb_api_key": "", "omdb_api_key": "", "region": "US", "language": "en-US",
			"services": defaultServices, "sonarr_url": "", "sonarr_api_key": "", "sonarr_quality_profile": "", "sonarr_root_folder": ""}
		b, _ := json.MarshalIndent(starter, "", "  ")
		os.WriteFile(path, b, 0o644)
	} else if err := json.Unmarshal(b, &cfg); err != nil {
		log.Printf("config.json is not valid JSON: %v", err)
		cfg = defaultConfig()
		cfg.Problem = configProblem(b, err)
	}
	cfg.TMDBKey = strings.TrimSpace(cfg.TMDBKey)
	cfg.OMDbKey = strings.TrimSpace(cfg.OMDbKey)
	if strings.HasPrefix(cfg.TMDBKey, "PASTE") {
		cfg.TMDBKey = ""
	}
	if strings.HasPrefix(cfg.OMDbKey, "PASTE") {
		cfg.OMDbKey = ""
	}
	if cfg.TMDBKey == "" && rewindPath != "" {
		var rw struct {
			TMDB string `json:"tmdb_api_key"`
			OMDb string `json:"omdb_api_key"`
		}
		if rb, err := os.ReadFile(rewindPath); err == nil && json.Unmarshal(rb, &rw) == nil && strings.TrimSpace(rw.TMDB) != "" && !strings.HasPrefix(rw.TMDB, "PASTE") {
			cfg.TMDBKey = strings.TrimSpace(rw.TMDB)
			if cfg.OMDbKey == "" && !strings.HasPrefix(rw.OMDb, "PASTE") {
				cfg.OMDbKey = strings.TrimSpace(rw.OMDb)
			}
			cfg.Borrowed = true
		}
	}
	cfg.SonarrURL = normalizeURL(cfg.SonarrURL)
	cfg.SonarrKey = strings.TrimSpace(cfg.SonarrKey)
	if cfg.Region == "" {
		cfg.Region = "US"
	}
	cfg.Region = strings.ToUpper(cfg.Region)
	if cfg.Language == "" {
		cfg.Language = "en-US"
	}
	if cfg.RefreshHours <= 0 {
		cfg.RefreshHours = 6
	}
	if cfg.Services == nil {
		cfg.Services = defaultServices
	}
	return cfg
}

// normalizeURL accepts "192.168.4.34", "192.168.4.34:8989" or a full URL.
func normalizeURL(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	host := s[strings.Index(s, "://")+3:]
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if !strings.Contains(host, ":") && strings.HasPrefix(s, "http://") {
		s = strings.Replace(s, host, host+":8989", 1) // Sonarr's default port
	}
	return s
}

func main() {
	shot := flag.String("shot", "", "render a demo scene to PNG instead of running on the device")
	out := flag.String("out", "shot.png", "PNG path for -shot")
	w := flag.Int("w", 640, "screen width for -shot")
	h := flag.Int("h", 480, "screen height for -shot")
	bench := flag.Int("bench", 0, "with -shot: render the scene N times and print the average frame time")
	icon := flag.String("icon", "", "write the 74×74 OnionOS app icon to this PNG path and exit")
	flag.Parse()
	if *icon != "" {
		f, err := os.Create(*icon)
		if err != nil {
			log.Fatal(err)
		}
		png.Encode(f, appIcon(74))
		f.Close()
		return
	}

	env := newEnv()
	if *shot != "" {
		c := NewCanvas(*w, *h)
		ui := NewUI(env, c.W, c.H)
		demoScene(ui, *shot)
		ui.Draw(c)
		if *bench > 0 {
			t0 := time.Now()
			for i := 0; i < *bench; i++ {
				ui.frame++
				ui.Draw(c)
			}
			fmt.Printf("%s: %.1f ms/frame\n", *shot, float64(time.Since(t0).Microseconds())/1000/float64(*bench))
		}
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		png.Encode(f, c)
		f.Close()
		return
	}

	logPath := filepath.Join(env.DataDir, "cablebox.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 512<<10 {
		os.Rename(logPath, logPath+".1")
	}
	if logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); logf != nil {
		log.SetOutput(logf)
	}
	log.Printf("cablebox %s starting, region=%s sonarr=%s", version, env.Config.Region, env.Config.SonarrURL)

	fb, err := OpenFramebuffer()
	if err != nil {
		log.Fatalf("framebuffer: %v", err)
	}
	defer fb.Close()
	c := NewCanvas(fb.W, fb.H)
	keys := make(chan int, 32)
	if err := ReadInput("/dev/input/event0", keys); err != nil {
		log.Fatalf("input: %v", err)
	}
	ui := NewUI(env, c.W, c.H)
	ui.Start()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	lastMin := -1
	dirty := true
	for !ui.quit {
		select {
		case k := <-keys:
			ui.Key(k)
			dirty = true
		case f := <-ui.post:
			f()
			dirty = true
		case <-tick.C:
			ui.frame++
			ui.Tick()
			if ui.Animating() {
				dirty = true
			}
			if m := time.Now().Minute(); m != lastMin {
				lastMin = m
				dirty = true
			}
		}
		if dirty {
			ui.Draw(c)
			fb.Present(c)
			dirty = false
		}
	}
	ui.Stop()
	// power off: the picture collapses to a dot
	ui.scr = scrBye
	for i := 0; i < 14; i++ {
		ui.frame++
		ui.byeFrame = i
		ui.Draw(c)
		fb.Present(c)
		time.Sleep(50 * time.Millisecond)
	}
	fb.Clear()
}

func configProblem(b []byte, err error) string {
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off := min(int(se.Offset), len(b))
		line := 1 + bytes.Count(b[:max(0, off-1)], []byte("\n"))
		if strings.Contains(se.Error(), "after object key:value pair") {
			prev := line - 1
			for prev > 1 && strings.TrimSpace(lineOf(b, prev)) == "" {
				prev--
			}
			return fmt.Sprintf("config.json has a missing comma at the end of line %d:\n%s\n\nAdd a comma there, save, and open Cablebox again.", prev, strings.TrimSpace(lineOf(b, prev)))
		}
		return fmt.Sprintf("config.json has a mistake near line %d (%s):\n%s\n\nFix it, save, and open Cablebox again.", line, se.Error(), strings.TrimSpace(lineOf(b, line)))
	case errors.As(err, &te):
		return fmt.Sprintf("In config.json, %q should be a %s, not a %s.", te.Field, te.Type, te.Value)
	}
	return "config.json couldn't be read: " + err.Error()
}

func lineOf(b []byte, n int) string {
	lines := strings.Split(string(b), "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}
