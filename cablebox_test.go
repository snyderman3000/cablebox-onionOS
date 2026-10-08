package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() { waitPoll = 20 * time.Millisecond }

// ---------- a fake Sonarr ----------

type fakeSonarr struct {
	mu       sync.Mutex
	v3       bool // has language profiles
	series   map[int]map[string]any
	episodes map[int][]SonarrEpisode
	nextID   int
	calls    []string
	bodies   map[string][]map[string]any // by method+path
}

func newFakeSonarr() *fakeSonarr {
	return &fakeSonarr{series: map[int]map[string]any{}, episodes: map[int][]SonarrEpisode{}, nextID: 1, bodies: map[string][]map[string]any{}}
}

func (f *fakeSonarr) last(key string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.bodies[key]
	if len(b) == 0 {
		return nil
	}
	return b[len(b)-1]
}

func (f *fakeSonarr) all(key string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[key]
}

func (f *fakeSonarr) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			w.WriteHeader(401)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		var body map[string]any
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			if len(b) > 0 {
				json.Unmarshal(b, &body)
				f.bodies[r.Method+" "+r.URL.Path] = append(f.bodies[r.Method+" "+r.URL.Path], body)
			}
		}
		js := func(v any) { json.NewEncoder(w).Encode(v) }
		p := r.URL.Path
		switch {
		case p == "/api/v3/qualityprofile":
			js([]map[string]any{{"id": 4, "name": "HD-1080p"}, {"id": 6, "name": "Ultra-HD"}})
		case p == "/api/v3/rootfolder":
			js([]map[string]any{{"path": "/media/TV", "freeSpace": 1 << 40}})
		case p == "/api/v3/languageprofile":
			if f.v3 {
				js([]map[string]any{{"id": 1, "name": "English"}})
			} else {
				w.WriteHeader(404)
			}
		case p == "/api/v3/series/lookup":
			term := r.URL.Query().Get("term")
			res := map[string]any{"title": "Harbor Lights", "tvdbId": 70000, "year": 2023, "seasons": []any{
				map[string]any{"seasonNumber": 0, "monitored": true}, map[string]any{"seasonNumber": 1, "monitored": true},
				map[string]any{"seasonNumber": 2, "monitored": true}, map[string]any{"seasonNumber": 3, "monitored": true}}}
			for id, s := range f.series {
				if s["tvdbId"] == float64(70000) {
					res["id"] = id
				}
			}
			if term == "tvdb:0" {
				js([]any{})
				return
			}
			js([]any{res})
		case p == "/api/v3/series" && r.Method == "POST":
			id := f.nextID
			f.nextID++
			body["id"] = float64(id)
			f.series[id] = body
			var eps []SonarrEpisode
			n := 100
			for s := 1; s <= 3; s++ {
				for e := 1; e <= 4; e++ {
					eps = append(eps, SonarrEpisode{ID: n, Season: s, Number: e, Monitored: false})
					n++
				}
			}
			f.episodes[id] = eps
			js(body)
		case p == "/api/v3/series" && r.Method == "GET":
			var out []any
			for _, s := range f.series {
				out = append(out, s)
			}
			js(out)
		case strings.HasPrefix(p, "/api/v3/series/"):
			var id int
			fmt.Sscanf(strings.TrimPrefix(p, "/api/v3/series/"), "%d", &id)
			if r.Method == "PUT" {
				f.series[id] = body
			}
			js(f.series[id])
		case p == "/api/v3/episode":
			var id int
			fmt.Sscanf(r.URL.Query().Get("seriesId"), "%d", &id)
			js(f.episodes[id])
		case p == "/api/v3/episode/monitor":
			ids := map[int]bool{}
			for _, x := range body["episodeIds"].([]any) {
				ids[int(x.(float64))] = true
			}
			on := body["monitored"].(bool)
			for sid, eps := range f.episodes {
				for i := range eps {
					if ids[eps[i].ID] {
						f.episodes[sid][i].Monitored = on
					}
				}
			}
			js([]any{})
		case p == "/api/v3/command" && r.Method == "GET":
			js([]any{})
		case p == "/api/v3/command" && r.Method == "POST", p == "/api/v3/seasonpass":
			js(map[string]any{"id": 1})
		default:
			t.Logf("fake sonarr: unhandled %s %s", r.Method, p)
			w.WriteHeader(404)
		}
	})
}

func sonarrEnv(t *testing.T, f *fakeSonarr) *Env {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	cfg := defaultConfig()
	cfg.SonarrURL, cfg.SonarrKey = srv.URL, "key"
	return &Env{DataDir: t.TempDir(), Config: cfg, HTTP: srv.Client(), Now: time.Now}
}

func testShow() *Show {
	return &Show{ID: 8000, Name: "Harbor Lights", TVDBID: 70000, FirstAir: "2023-03-14", Status: "Returning Series", RT: -1, Meta: -1}
}

func noStep(string) {}

func monitoredSet(f *fakeSonarr, id int) map[EpKey]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[EpKey]bool{}
	for _, e := range f.episodes[id] {
		if e.Monitored {
			out[EpKey{e.Season, e.Number}] = true
		}
	}
	return out
}

func TestAddFutureAndAll(t *testing.T) {
	f := newFakeSonarr()
	f.v3 = true
	env := sonarrEnv(t, f)
	ctx := context.Background()
	res, err := env.AddShow(ctx, testShow(), Order{Mode: orderFuture}, noStep)
	if err != nil || res.SeriesID != 1 {
		t.Fatalf("future: %v %+v", err, res)
	}
	b := f.last("POST /api/v3/series")
	opts := b["addOptions"].(map[string]any)
	if opts["monitor"] != "future" || opts["searchForMissingEpisodes"] != false {
		t.Errorf("future add options: %v", opts)
	}
	if b["qualityProfileId"] != float64(4) || b["rootFolderPath"] != "/media/TV" || b["languageProfileId"] != float64(1) || b["seasonFolder"] != true {
		t.Errorf("target fields: %v", b)
	}
	for _, s := range b["seasons"].([]any) {
		if s.(map[string]any)["monitored"] != false {
			t.Errorf("future: seasons should start unmonitored: %v", s)
		}
	}

	f2 := newFakeSonarr()
	env2 := sonarrEnv(t, f2)
	env2.Config.SonarrProfile = "ultra-hd"
	if _, err := env2.AddShow(ctx, testShow(), Order{Mode: orderAll}, noStep); err != nil {
		t.Fatal(err)
	}
	b = f2.last("POST /api/v3/series")
	opts = b["addOptions"].(map[string]any)
	if opts["monitor"] != "all" || opts["searchForMissingEpisodes"] != true {
		t.Errorf("all add options: %v", opts)
	}
	if _, ok := b["languageProfileId"]; ok {
		t.Errorf("Sonarr v4 shouldn't get a language profile")
	}
	if b["qualityProfileId"] != float64(6) {
		t.Errorf("profile by name: %v", b["qualityProfileId"])
	}
	seasons := b["seasons"].([]any)
	if seasons[0].(map[string]any)["monitored"] != false || seasons[2].(map[string]any)["monitored"] != true {
		t.Errorf("all: specials off, seasons on: %v", seasons)
	}
}

func TestAddChosenSeasons(t *testing.T) {
	f := newFakeSonarr()
	env := sonarrEnv(t, f)
	res, err := env.AddShow(context.Background(), testShow(), Order{Mode: orderSeasons, Seasons: []int{2}}, noStep)
	if err != nil {
		t.Fatal(err)
	}
	if opts := f.last("POST /api/v3/series")["addOptions"].(map[string]any); opts["monitor"] != "none" {
		t.Errorf("chosen seasons add with monitor none: %v", opts)
	}
	got := monitoredSet(f, res.SeriesID)
	if len(got) != 4 || !got[EpKey{2, 1}] || got[EpKey{1, 1}] {
		t.Errorf("season 2 only: %v", got)
	}
	put := f.last("PUT /api/v3/series/1")
	if put["monitored"] != true {
		t.Errorf("series must stay monitored")
	}
	for _, s := range put["seasons"].([]any) {
		m := s.(map[string]any)
		if (m["seasonNumber"] == float64(2)) != (m["monitored"] == true) {
			t.Errorf("season flags: %v", m)
		}
	}
	cmd := f.last("POST /api/v3/command")
	if cmd["name"] != "SeasonSearch" || cmd["seasonNumber"] != float64(2) {
		t.Errorf("search: %v", cmd)
	}
	if !strings.Contains(res.Summary, "season 2") {
		t.Errorf("summary: %q", res.Summary)
	}
}

func TestAddChosenEpisodes(t *testing.T) {
	f := newFakeSonarr()
	env := sonarrEnv(t, f)
	o := Order{Mode: orderEpisodes, Episodes: []EpKey{{1, 2}, {3, 4}, {9, 9}}}
	res, err := env.AddShow(context.Background(), testShow(), o, noStep)
	if err != nil {
		t.Fatal(err)
	}
	got := monitoredSet(f, res.SeriesID)
	if len(got) != 2 || !got[EpKey{1, 2}] || !got[EpKey{3, 4}] {
		t.Errorf("chosen episodes: %v", got)
	}
	cmd := f.last("POST /api/v3/command")
	if cmd["name"] != "EpisodeSearch" || len(cmd["episodeIds"].([]any)) != 2 {
		t.Errorf("episode search: %v", cmd)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "1 chosen episode") {
		t.Errorf("the missing 9x09 should be noted: %v", res.Notes)
	}
}

func TestChangeExistingShow(t *testing.T) {
	f := newFakeSonarr()
	env := sonarrEnv(t, f)
	ctx := context.Background()
	if _, err := env.AddShow(ctx, testShow(), Order{Mode: orderEpisodes, Episodes: []EpKey{{1, 1}}}, noStep); err != nil {
		t.Fatal(err)
	}
	before, _ := env.SonarrEpisodes(ctx, 1)
	// now also want 1x02; 1x01 is already monitored so only 1x02 is searched
	res, err := env.ChangeShow(ctx, testShow(), 1, Order{Mode: orderEpisodes, Episodes: []EpKey{{1, 1}, {1, 2}}}, before, noStep)
	if err != nil {
		t.Fatal(err)
	}
	cmd := f.last("POST /api/v3/command")
	ids := cmd["episodeIds"].([]any)
	if len(ids) != 1 || ids[0] != float64(101) {
		t.Errorf("only the new episode is searched: %v", cmd)
	}
	if got := monitoredSet(f, 1); len(got) != 2 {
		t.Errorf("monitored: %v", got)
	}
	_ = res
	// ordering a show that's already there goes through ChangeShow
	if _, err := env.AddShow(ctx, testShow(), Order{Mode: orderFuture}, noStep); err != nil {
		t.Fatal(err)
	}
	sp := f.last("POST /api/v3/seasonpass")
	if sp == nil || sp["monitoringOptions"].(map[string]any)["monitor"] != "future" {
		t.Errorf("existing show → season pass: %v", sp)
	}
	if n := len(f.all("POST /api/v3/series")); n != 1 {
		t.Errorf("an existing show must not be added twice (%d adds)", n)
	}
}

func TestSonarrErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `[{"propertyName":"Path","errorMessage":"Path is already configured for an existing series"}]`)
	}))
	defer srv.Close()
	env := &Env{Config: Config{SonarrURL: srv.URL, SonarrKey: "k"}, HTTP: srv.Client()}
	err := env.sonarrDo(context.Background(), "POST", "/api/v3/series", map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Errorf("validation message: %v", err)
	}
}

// ---------- a fake TMDB ----------

func fakeTMDB(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		js := func(s string) { fmt.Fprint(w, s) }
		switch {
		case r.URL.Path == "/watch/providers/tv":
			js(`{"results":[{"provider_id":8,"provider_name":"Netflix","display_priorities":{"US":1}},
				{"provider_id":1796,"provider_name":"Netflix basic with Ads","display_priorities":{"US":30}},
				{"provider_id":531,"provider_name":"Paramount Plus","display_priorities":{"US":5}},
				{"provider_id":2303,"provider_name":"Paramount Plus Premium","display_priorities":{"US":6}},
				{"provider_id":1899,"provider_name":"HBO Max","display_priorities":{"US":3}}]}`)
		case r.URL.Path == "/genre/tv/list":
			js(`{"genres":[{"id":18,"name":"Drama"},{"id":35,"name":"Comedy"}]}`)
		case r.URL.Path == "/discover/tv":
			if r.URL.Query().Get("page") != "1" {
				js(`{"page":2,"total_pages":1,"results":[]}`)
				return
			}
			js(`{"page":1,"total_pages":1,"results":[{"id":8000,"name":"Harbor Lights","first_air_date":"2023-03-14","vote_average":8.1,"vote_count":400,"genre_ids":[80]},
				{"id":8001,"name":"Cold Open","first_air_date":"2025-01-02","vote_average":7.5,"vote_count":200}]}`)
		case strings.HasPrefix(r.URL.Path, "/tv/8000/season/"):
			js(`{"episodes":[{"season_number":2,"episode_number":1,"name":"Low Tide","air_date":"2024-03-14"},{"season_number":2,"episode_number":2,"name":"Fog Horn","air_date":"2024-03-21"}]}`)
		case strings.HasPrefix(r.URL.Path, "/tv/"):
			js(`{"id":8000,"name":"Harbor Lights","status":"Returning Series","first_air_date":"2023-03-14","number_of_seasons":3,"number_of_episodes":24,
				"episode_run_time":[52],"genres":[{"id":80,"name":"Crime"}],"networks":[{"name":"Netflix"}],
				"seasons":[{"season_number":0,"episode_count":2},{"season_number":1,"episode_count":8},{"season_number":2,"episode_count":8},{"season_number":3,"episode_count":8}],
				"external_ids":{"tvdb_id":70000,"imdb_id":"tt0000001"},
				"content_ratings":{"results":[{"iso_3166_1":"US","rating":"TV-MA"}]},
				"credits":{"cast":[{"name":"Ada Okafor","order":1,"character":"Captain"}]},
				"videos":{"results":[{"key":"abc","site":"YouTube","type":"Trailer","official":true,"iso_639_1":"en"}]},
				"watch/providers":{"results":{"US":{"flatrate":[{"provider_id":1796,"provider_name":"Netflix basic with Ads"},{"provider_id":8,"provider_name":"Netflix"}]}}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestTMDB(t *testing.T) {
	srv := fakeTMDB(t)
	defer srv.Close()
	env := &Env{Config: defaultConfig(), HTTP: srv.Client(), TMDBBase: srv.URL, Now: time.Now}
	env.Config.TMDBKey = "k"
	ctx := context.Background()
	svcs, err := env.Services(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ids := matchService("Netflix", svcs); fmt.Sprint(ids) != "[8 1796]" {
		t.Errorf("netflix ids %v", ids)
	}
	if ids := matchService("Paramount+", svcs); fmt.Sprint(ids) != "[531 2303]" {
		t.Errorf("paramount ids %v", ids)
	}
	lu := Lineup(defaultServices, svcs, map[int]string{18: "Drama"})
	var names []string
	for _, ch := range lu {
		if ch.Kind == chService {
			names = append(names, ch.Name)
		}
	}
	if strings.Join(names, ",") != "NETFLIX,HBO MAX,PARAMOUNT+" {
		t.Errorf("services offered in the region only: %v", names)
	}
	q := env.channelQuery(lu[4], 1, false)
	if q.Get("with_watch_providers") != "8|1796" || q.Get("watch_region") != "US" {
		t.Errorf("service query %v", q)
	}
	shows, err := env.Discover(ctx, lu[0], 40, nil)
	if err != nil || len(shows) != 2 {
		t.Fatalf("discover %v %d", err, len(shows))
	}
	s := shows[0]
	if err := env.Details(ctx, s); err != nil {
		t.Fatal(err)
	}
	if s.TVDBID != 70000 || s.Cert != "TV-MA" || s.Trailer != "abc" || len(s.RealSeasons()) != 3 || s.Runtime != 52 {
		t.Errorf("details %+v", s)
	}
	if len(s.Cast) != 1 || s.Cast[0].Role != "Captain" {
		t.Errorf("cast from credits: %+v", s.Cast)
	}
	if strings.Join(s.Providers, ",") != "Netflix" {
		t.Errorf("providers merged: %v", s.Providers)
	}
	if s.Years() != "2023–" || !strings.Contains(s.MetaLine(), "3 SEASONS") {
		t.Errorf("meta %q %q", s.Years(), s.MetaLine())
	}
	eps, err := env.Season(ctx, 8000, 2)
	if err != nil || len(eps) != 2 || eps[1].Name != "Fog Horn" {
		t.Errorf("season %v %v", eps, err)
	}
}

func TestBorrowRewindKeys(t *testing.T) {
	dir := t.TempDir()
	rw := filepath.Join(dir, "rewind.json")
	os.WriteFile(rw, []byte(`{"tmdb_api_key":"tm","omdb_api_key":"om"}`), 0o644)
	cfg := loadConfig(filepath.Join(dir, "config.json"), rw)
	if cfg.TMDBKey != "tm" || cfg.OMDbKey != "om" || !cfg.Borrowed {
		t.Errorf("borrow: %+v", cfg)
	}
	os.WriteFile(filepath.Join(dir, "example.json"), []byte(`{"tmdb_api_key":"PASTE YOUR TMDB API KEY HERE","omdb_api_key":"PASTE YOUR OMDB API KEY HERE"}`), 0o644)
	cfg = loadConfig(filepath.Join(dir, "example.json"), rw)
	if cfg.TMDBKey != "tm" || cfg.OMDbKey != "om" {
		t.Errorf("an untouched example config should borrow both keys: %+v", cfg)
	}
	os.WriteFile(filepath.Join(dir, "own.json"), []byte(`{"tmdb_api_key":"mine","sonarr_url":"192.168.4.34"}`), 0o644)
	cfg = loadConfig(filepath.Join(dir, "own.json"), rw)
	if cfg.TMDBKey != "mine" || cfg.Borrowed || cfg.SonarrURL != "http://192.168.4.34:8989" {
		t.Errorf("own key and url: %+v", cfg)
	}
}

func TestScenesDraw(t *testing.T) {
	scenes := []string{"guide", "tune", "loading", "detail", "detail-owned", "lineup", "order", "manage", "seasons", "episodes",
		"progress", "done", "trailer", "nosonarr", "setup", "bye"}
	for _, size := range [][2]int{{640, 480}, {752, 560}} {
		for _, s := range scenes {
			env := &Env{DataDir: t.TempDir(), Config: defaultConfig(), Now: time.Now}
			c := NewCanvas(size[0], size[1])
			u := NewUI(env, c.W, c.H)
			demoScene(u, s)
			for i := 0; i < 3; i++ {
				u.frame++
				u.Draw(c)
			}
		}
	}
}

func drain(t *testing.T, u *UI, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case f := <-u.post:
			f()
		case <-time.After(20 * time.Millisecond):
			u.frame++
			u.Tick()
		case <-deadline:
			t.Fatalf("timed out")
		}
	}
}

// From the guide: tune, open a show, choose two episodes, order them.
func TestOrderFlow(t *testing.T) {
	tm := fakeTMDB(t)
	defer tm.Close()
	f := newFakeSonarr()
	env := sonarrEnv(t, f)
	env.TMDBBase = tm.URL
	env.ImageBase = tm.URL + "/img/"
	env.Config.TMDBKey = "k"
	env.HTTP = &http.Client{}
	u := NewUI(env, 640, 480)
	u.Start()
	drain(t, u, func() bool { return len(u.shows) == 2 && u.lib != nil })
	drain(t, u, func() bool { return !u.shows[0].DetailAt.IsZero() })
	if len(u.lineup) < 10 {
		t.Errorf("lineup should include services and genres once loaded: %d", len(u.lineup))
	}
	u.Key(BtnX)
	if u.modal != modalOrder || len(u.ord.items) != 4 {
		t.Fatalf("order menu: %v %d", u.modal, len(u.ord.items))
	}
	u.ord.sel = 3 // choose episodes
	u.Key(BtnA)
	if u.scr != scrPick || u.pick.mode != orderEpisodes {
		t.Fatalf("episode picker")
	}
	u.Key(BtnRight) // season 2
	drain(t, u, func() bool { return len(u.curEpisodes()) == 2 })
	u.Key(BtnA) // 2x01, moves down
	u.Key(BtnA) // 2x02
	if u.pickCount() != 2 {
		t.Fatalf("chosen %d", u.pickCount())
	}
	u.Key(BtnStart)
	if u.modal != modalProgress {
		t.Fatalf("progress")
	}
	drain(t, u, func() bool { return u.prog.done })
	if u.prog.err != "" {
		t.Fatalf("order failed: %s", u.prog.err)
	}
	if got := monitoredSet(f, 1); len(got) != 2 || !got[EpKey{2, 1}] || !got[EpKey{2, 2}] {
		t.Errorf("monitored after order: %v", got)
	}
	u.Key(BtnA)
	if u.scr != scrDetail {
		t.Errorf("after ordering, back to the show")
	}
	// channel up/down and the guide
	ch := u.chIdx
	u.Key(BtnB)
	u.Key(BtnR1)
	if u.chIdx != ch+1 {
		t.Errorf("R1 = channel up")
	}
	u.Key(BtnStart)
	if u.scr != scrLineup {
		t.Errorf("START opens the channel guide")
	}
}

// If TMDB empties (or rejects) the filtered listing, the plain query is used.
func TestDiscoverFallback(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("with_watch_monetization_types") != "" {
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
			return
		}
		fmt.Fprint(w, `{"page":1,"total_pages":1,"results":[{"id":1,"name":"Plain Show"}]}`)
	}))
	defer srv.Close()
	env := &Env{Config: defaultConfig(), HTTP: srv.Client(), TMDBBase: srv.URL, Now: time.Now}
	env.Config.TMDBKey = "k"
	shows, err := env.Discover(context.Background(), &Channel{Num: 4, Name: "POPULAR", Kind: chPopular}, 40, nil)
	if err != nil || len(shows) != 1 || len(queries) != 2 {
		t.Fatalf("fallback: %v %d %v", err, len(shows), queries)
	}
	if !strings.Contains(queries[0], "without_genres=10767%7C10763%7C10766") {
		t.Errorf("talk, news and soaps are excluded with OR: %s", queries[0])
	}
	q := env.channelQuery(&Channel{Kind: chGenre, Genre: 10767}, 1, false)
	if q.Get("without_genres") != "10763|10766" {
		t.Errorf("a genre channel doesn't exclude itself: %q", q.Get("without_genres"))
	}
}

// While a channel is loading, details wait, so the listing isn't starved.
func TestDetailsWaitForListing(t *testing.T) {
	env := &Env{DataDir: t.TempDir(), Config: defaultConfig(), Now: time.Now}
	u := NewUI(env, 640, 480)
	demoScene(u, "guide")
	for _, s := range u.shows {
		s.DetailAt = time.Time{}
	}
	u.chLoading["new"] = true
	u.dispatchDetails()
	if len(u.detBusy) != 0 {
		t.Errorf("details started while a channel was loading")
	}
	delete(u.chLoading, "new")
	u.env.HTTP = &http.Client{Timeout: time.Millisecond}
	u.dispatchDetails()
	if len(u.detBusy) != 2 {
		t.Errorf("two detail lookups at a time, got %d", len(u.detBusy))
	}
}

func TestRedactKey(t *testing.T) {
	err := redactKey(fmt.Errorf(`Get "https://x/?api_key=secret123": timeout`), "secret123")
	if strings.Contains(err.Error(), "secret123") || !strings.Contains(err.Error(), "api_key=***") {
		t.Errorf("redact: %v", err)
	}
}

// TMDB's CDN speaks HTTP/2; the client must still get plain HTTP/1.1 answers.
func TestTransportAgainstHTTP2Server(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"proto":%q}`, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	tr := newTransport()
	tr.TLSClientConfig.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	resp, err := (&http.Client{Transport: tr}).Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "HTTP/1.1") {
		t.Errorf("expected HTTP/1.1, got %s", b)
	}
}
