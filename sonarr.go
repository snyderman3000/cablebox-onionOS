package main

// Sonarr (v3 and v4 API) on the home network: which shows are already in the
// library, adding a show with the episodes you choose, and changing what an
// existing show monitors.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (env *Env) SonarrOn() bool { return env.Config.SonarrURL != "" && env.Config.SonarrKey != "" }

type SonarrStats struct {
	EpisodeFileCount  int     `json:"episodeFileCount"`
	EpisodeCount      int     `json:"episodeCount"`
	TotalEpisodeCount int     `json:"totalEpisodeCount"`
	SizeOnDisk        int64   `json:"sizeOnDisk"`
	Percent           float64 `json:"percentOfEpisodes"`
}

type SonarrSeason struct {
	Number    int          `json:"seasonNumber"`
	Monitored bool         `json:"monitored"`
	Stats     *SonarrStats `json:"statistics"`
}

type SonarrSeries struct {
	ID        int            `json:"id"`
	Title     string         `json:"title"`
	TVDBID    int            `json:"tvdbId"`
	TMDBID    int            `json:"tmdbId"`
	Monitored bool           `json:"monitored"`
	Seasons   []SonarrSeason `json:"seasons"`
	Stats     SonarrStats    `json:"statistics"`
}

type SonarrEpisode struct {
	ID        int    `json:"id"`
	Season    int    `json:"seasonNumber"`
	Number    int    `json:"episodeNumber"`
	Title     string `json:"title"`
	AirDate   string `json:"airDate"`
	Monitored bool   `json:"monitored"`
	HasFile   bool   `json:"hasFile"`
}

// sonarrDo calls the API. body and out may be nil.
func (env *Env) sonarrDo(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(env.Config.SonarrURL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", env.Config.SonarrKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := env.HTTP.Do(req)
	if err != nil {
		return netHint(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return netHint(err)
	}
	switch {
	case resp.StatusCode == 401:
		return &APIError{"Sonarr", 401, "Sonarr rejected the API key — check sonarr_api_key (Sonarr → Settings → General)"}
	case resp.StatusCode == 404:
		return &APIError{"Sonarr", 404, "Sonarr doesn't have " + strings.SplitN(path, "?", 2)[0] + " — is sonarr_url right (with any URL base, like /sonarr)?"}
	case resp.StatusCode >= 300:
		return &APIError{"Sonarr", resp.StatusCode, "Sonarr: " + sonarrMessage(b, resp.Status)}
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return errors.New("that address isn't answering like Sonarr — check sonarr_url")
	}
	return nil
}

// sonarrMessage pulls the reason out of Sonarr's error bodies (validation lists or {"message"}).
func sonarrMessage(b []byte, status string) string {
	var list []struct {
		Msg string `json:"errorMessage"`
	}
	if json.Unmarshal(b, &list) == nil && len(list) > 0 && list[0].Msg != "" {
		var ms []string
		for _, x := range list {
			ms = append(ms, x.Msg)
		}
		return strings.Join(ms, "; ")
	}
	var one struct {
		Msg string `json:"message"`
	}
	if json.Unmarshal(b, &one) == nil && one.Msg != "" {
		return one.Msg
	}
	if s := strings.TrimSpace(string(b)); s != "" && len(s) < 200 {
		return s
	}
	return status
}

// SonarrLibrary maps the shows already in Sonarr by TVDB id and by TMDB id.
type SonarrLibrary struct {
	ByTVDB map[int]*SonarrSeries
	ByTMDB map[int]*SonarrSeries
}

func (l *SonarrLibrary) Find(s *Show) *SonarrSeries {
	if l == nil || s == nil {
		return nil
	}
	if s.TVDBID > 0 {
		if x := l.ByTVDB[s.TVDBID]; x != nil {
			return x
		}
	}
	return l.ByTMDB[s.ID]
}

func (env *Env) SonarrLibrary(ctx context.Context) (*SonarrLibrary, error) {
	var ss []*SonarrSeries
	if err := env.sonarrDo(ctx, "GET", "/api/v3/series", nil, &ss); err != nil {
		return nil, err
	}
	lib := &SonarrLibrary{ByTVDB: map[int]*SonarrSeries{}, ByTMDB: map[int]*SonarrSeries{}}
	for _, s := range ss {
		if s.TVDBID > 0 {
			lib.ByTVDB[s.TVDBID] = s
		}
		if s.TMDBID > 0 {
			lib.ByTMDB[s.TMDBID] = s
		}
	}
	return lib, nil
}

type SonarrTarget struct {
	Profile     string
	ProfileID   int
	Root        string
	LanguageID  int
	FreeSpace   int64
	SearchOnAdd bool
}

// SonarrTarget resolves the quality profile, root folder (and v3 language profile) to use.
func (env *Env) SonarrTarget(ctx context.Context) (*SonarrTarget, error) {
	var profiles []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := env.sonarrDo(ctx, "GET", "/api/v3/qualityprofile", nil, &profiles); err != nil {
		return nil, err
	}
	var roots []struct {
		Path       string `json:"path"`
		FreeSpace  int64  `json:"freeSpace"`
		Accessible bool   `json:"accessible"`
	}
	if err := env.sonarrDo(ctx, "GET", "/api/v3/rootfolder", nil, &roots); err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, errors.New("Sonarr has no quality profiles")
	}
	if len(roots) == 0 {
		return nil, errors.New("Sonarr has no root folder yet — add one under Settings → Media Management")
	}
	cfg := env.Config
	t := &SonarrTarget{SearchOnAdd: cfg.search()}
	pick := profiles[0]
	if want := strings.TrimSpace(cfg.SonarrProfile); want != "" {
		found := false
		for _, p := range profiles {
			if strings.EqualFold(p.Name, want) || strconv.Itoa(p.ID) == want {
				pick, found = p, true
			}
		}
		if !found {
			return nil, fmt.Errorf("Sonarr has no quality profile called %q", want)
		}
	}
	t.Profile, t.ProfileID = pick.Name, pick.ID
	root := roots[0]
	if want := strings.TrimRight(strings.TrimSpace(cfg.SonarrRoot), "/"); want != "" {
		found := false
		for _, r := range roots {
			if strings.TrimRight(r.Path, "/") == want {
				root, found = r, true
			}
		}
		if !found {
			return nil, fmt.Errorf("Sonarr has no root folder %q", want)
		}
	}
	t.Root, t.FreeSpace = root.Path, root.FreeSpace
	// Sonarr v3 needs a language profile; v4 dropped them
	var langs []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if env.sonarrDo(ctx, "GET", "/api/v3/languageprofile", nil, &langs) == nil && len(langs) > 0 {
		t.LanguageID = langs[0].ID
		for _, l := range langs {
			if strings.EqualFold(l.Name, cfg.SonarrLang) || strconv.Itoa(l.ID) == cfg.SonarrLang {
				t.LanguageID = l.ID
			}
		}
	}
	return t, nil
}

func (env *Env) SonarrEpisodes(ctx context.Context, seriesID int) ([]SonarrEpisode, error) {
	var eps []SonarrEpisode
	err := env.sonarrDo(ctx, "GET", "/api/v3/episode?seriesId="+strconv.Itoa(seriesID), nil, &eps)
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].Season != eps[j].Season {
			return eps[i].Season < eps[j].Season
		}
		return eps[i].Number < eps[j].Number
	})
	return eps, err
}

// ---------- ordering ----------

type OrderMode int

const (
	orderFuture   OrderMode = iota // new episodes from now on
	orderAll                       // every episode
	orderSeasons                   // chosen seasons
	orderEpisodes                  // chosen episodes
	orderSearch                    // existing show: search for what's missing
)

type EpKey struct{ S, E int }

type Order struct {
	Mode     OrderMode
	Seasons  []int
	Episodes []EpKey
}

// OrderResult is what the progress screen reports at the end.
type OrderResult struct {
	SeriesID int
	Summary  string
	Notes    []string
}

// isAnime marks Japanese animation so Sonarr uses absolute numbering for it.
func isAnime(s *Show) bool {
	return s.Lang == "ja" && contains(s.GenreIDs, 16)
}

// AddShow adds a show to Sonarr and sets up monitoring for the order.
func (env *Env) AddShow(ctx context.Context, s *Show, o Order, step func(string)) (*OrderResult, error) {
	step("LOOKING UP " + strings.ToUpper(s.Name))
	t, err := env.SonarrTarget(ctx)
	if err != nil {
		return nil, err
	}
	res, err := env.sonarrLookup(ctx, s)
	if err != nil {
		return nil, err
	}
	if id, _ := res["id"].(float64); id > 0 {
		return env.ChangeShow(ctx, s, int(id), o, nil, step)
	}
	res["qualityProfileId"] = t.ProfileID
	if t.LanguageID > 0 {
		res["languageProfileId"] = t.LanguageID
	}
	res["rootFolderPath"] = t.Root
	res["monitored"] = true
	res["seasonFolder"] = true
	if isAnime(s) {
		res["seriesType"] = "anime"
	} else if _, ok := res["seriesType"]; !ok {
		res["seriesType"] = "standard"
	}
	chosen := map[int]bool{}
	for _, n := range o.Seasons {
		chosen[n] = true
	}
	if seasons, ok := res["seasons"].([]any); ok {
		for _, x := range seasons {
			if m, ok := x.(map[string]any); ok {
				n, _ := m["seasonNumber"].(float64)
				switch o.Mode {
				case orderAll:
					m["monitored"] = n > 0
				case orderSeasons:
					m["monitored"] = chosen[int(n)]
				default:
					m["monitored"] = false
				}
			}
		}
	}
	opts := map[string]any{"searchForMissingEpisodes": false, "searchForCutoffUnmetEpisodes": false}
	switch o.Mode {
	case orderFuture:
		opts["monitor"] = "future"
	case orderAll:
		opts["monitor"] = "all"
		opts["searchForMissingEpisodes"] = t.SearchOnAdd
	default:
		// chosen seasons or episodes are applied once Sonarr has listed the episodes
		opts["monitor"] = "none"
	}
	res["addOptions"] = opts
	step("ADDING TO SONARR")
	var added struct {
		ID int `json:"id"`
	}
	if err := env.sonarrDo(ctx, "POST", "/api/v3/series", res, &added); err != nil {
		return nil, err
	}
	out := &OrderResult{SeriesID: added.ID}
	switch o.Mode {
	case orderFuture:
		out.Summary = "Added. Sonarr will get new episodes as they air."
		return out, nil
	case orderAll:
		out.Summary = "Added with every episode."
		if t.SearchOnAdd {
			out.Summary += " Sonarr is searching for them now."
		}
		return out, nil
	}
	step("WAITING FOR SONARR TO LIST EPISODES")
	eps, err := env.waitForEpisodes(ctx, added.ID)
	if err != nil {
		return out, fmt.Errorf("added, but %v", err)
	}
	r, err := env.applySelection(ctx, added.ID, o, eps, nil, t.SearchOnAdd, step)
	if r != nil {
		r.SeriesID = added.ID
		r.Summary = "Added. " + r.Summary
	}
	return r, err
}

// ChangeShow re-plans what an existing Sonarr series monitors. before holds
// the episodes as they were, so only newly chosen ones are searched.
func (env *Env) ChangeShow(ctx context.Context, s *Show, seriesID int, o Order, before []SonarrEpisode, step func(string)) (*OrderResult, error) {
	search := env.Config.search()
	out := &OrderResult{SeriesID: seriesID}
	switch o.Mode {
	case orderSearch:
		step("ASKING SONARR TO SEARCH")
		if err := env.sonarrCommand(ctx, map[string]any{"name": "SeriesSearch", "seriesId": seriesID}); err != nil {
			return nil, err
		}
		out.Summary = "Sonarr is searching for every missing episode it monitors."
		return out, nil
	case orderFuture, orderAll:
		step("CHANGING MONITORING")
		monitor := "future"
		if o.Mode == orderAll {
			monitor = "all"
		}
		body := map[string]any{"series": []any{map[string]any{"id": seriesID, "monitored": true}},
			"monitoringOptions": map[string]any{"monitor": monitor}}
		if err := env.sonarrDo(ctx, "POST", "/api/v3/seasonpass", body, nil); err != nil {
			return nil, err
		}
		if o.Mode == orderAll {
			out.Summary = "Now monitoring every episode."
			if search {
				env.sonarrCommand(ctx, map[string]any{"name": "SeriesSearch", "seriesId": seriesID})
				out.Summary += " Sonarr is searching for the missing ones."
			}
		} else {
			out.Summary = "Now monitoring new episodes only."
		}
		return out, nil
	}
	if before == nil {
		step("READING SONARR'S EPISODES")
		var err error
		if before, err = env.SonarrEpisodes(ctx, seriesID); err != nil {
			return nil, err
		}
	}
	was := map[int]bool{}
	for _, e := range before {
		if e.Monitored {
			was[e.ID] = true
		}
	}
	return env.applySelection(ctx, seriesID, o, before, was, search, step)
}

// applySelection monitors exactly the chosen seasons or episodes and searches for them.
func (env *Env) applySelection(ctx context.Context, seriesID int, o Order, eps []SonarrEpisode, was map[int]bool, search bool, step func(string)) (*OrderResult, error) {
	step("CHOOSING EPISODES")
	chosenSeason := map[int]bool{}
	for _, n := range o.Seasons {
		chosenSeason[n] = true
	}
	chosenEp := map[EpKey]bool{}
	for _, k := range o.Episodes {
		chosenEp[k] = true
	}
	var on, off []int
	found := map[EpKey]bool{}
	for _, e := range eps {
		k := EpKey{e.Season, e.Number}
		want := false
		if o.Mode == orderSeasons {
			want = chosenSeason[e.Season]
		} else {
			want = chosenEp[k]
		}
		if want {
			on = append(on, e.ID)
			found[k] = true
		} else {
			off = append(off, e.ID)
		}
	}
	// the series stays monitored; season flags follow the choice
	var series map[string]any
	if err := env.sonarrDo(ctx, "GET", "/api/v3/series/"+strconv.Itoa(seriesID), nil, &series); err != nil {
		return nil, err
	}
	series["monitored"] = true
	if seasons, ok := series["seasons"].([]any); ok {
		for _, x := range seasons {
			m, ok := x.(map[string]any)
			if !ok {
				continue
			}
			n, _ := m["seasonNumber"].(float64)
			if o.Mode == orderSeasons {
				m["monitored"] = chosenSeason[int(n)]
			} else {
				// a season counts as monitored if any chosen episode is in it
				hit := false
				for k := range chosenEp {
					if k.S == int(n) {
						hit = true
					}
				}
				m["monitored"] = hit
			}
		}
	}
	if err := env.sonarrDo(ctx, "PUT", "/api/v3/series/"+strconv.Itoa(seriesID), series, nil); err != nil {
		return nil, err
	}
	// set episodes explicitly, after the series update (which can cascade season flags)
	if len(off) > 0 {
		if err := env.sonarrDo(ctx, "PUT", "/api/v3/episode/monitor", map[string]any{"episodeIds": off, "monitored": false}, nil); err != nil {
			return nil, err
		}
	}
	if len(on) > 0 {
		if err := env.sonarrDo(ctx, "PUT", "/api/v3/episode/monitor", map[string]any{"episodeIds": on, "monitored": true}, nil); err != nil {
			return nil, err
		}
	}
	r := &OrderResult{SeriesID: seriesID}
	what := fmt.Sprintf("%d episode%s", len(on), strings.ToLower(plural(len(on))))
	if o.Mode == orderSeasons {
		ns := append([]int(nil), o.Seasons...)
		sort.Ints(ns)
		var ss []string
		for _, n := range ns {
			ss = append(ss, strconv.Itoa(n))
		}
		label := "season"
		if len(ns) > 1 {
			label = "seasons"
		}
		what = fmt.Sprintf("%s %s (%s)", label, strings.Join(ss, ", "), what)
	}
	r.Summary = "Monitoring " + what + "."
	if o.Mode == orderEpisodes {
		missing := 0
		for k := range chosenEp {
			if !found[k] {
				missing++
			}
		}
		if missing > 0 {
			r.Notes = append(r.Notes, fmt.Sprintf("%d chosen episode%s weren't in Sonarr's list — its numbering for this show differs from TMDB's. Check them in Sonarr.", missing, strings.ToLower(plural(missing))))
		}
	}
	if !search || len(on) == 0 {
		return r, nil
	}
	// search only for episodes that weren't already monitored
	var fresh []int
	for _, id := range on {
		if !was[id] {
			fresh = append(fresh, id)
		}
	}
	if len(fresh) == 0 {
		return r, nil
	}
	step("ASKING SONARR TO SEARCH")
	var err error
	if o.Mode == orderSeasons && was == nil {
		for _, n := range o.Seasons {
			if e := env.sonarrCommand(ctx, map[string]any{"name": "SeasonSearch", "seriesId": seriesID, "seasonNumber": n}); e != nil {
				err = e
			}
		}
	} else {
		err = env.sonarrCommand(ctx, map[string]any{"name": "EpisodeSearch", "episodeIds": fresh})
	}
	if err != nil {
		r.Notes = append(r.Notes, "The search didn't start: "+err.Error())
		return r, nil
	}
	r.Summary += " Sonarr is searching for them now."
	return r, nil
}

func (env *Env) sonarrCommand(ctx context.Context, body map[string]any) error {
	return env.sonarrDo(ctx, "POST", "/api/v3/command", body, nil)
}

// sonarrLookup finds the show in Sonarr's metadata (which may say it's already added).
func (env *Env) sonarrLookup(ctx context.Context, s *Show) (map[string]any, error) {
	var res []map[string]any
	if s.TVDBID > 0 {
		if err := env.sonarrDo(ctx, "GET", "/api/v3/series/lookup?term="+url.QueryEscape("tvdb:"+strconv.Itoa(s.TVDBID)), nil, &res); err != nil {
			return nil, err
		}
		if len(res) > 0 {
			return res[0], nil
		}
	}
	if err := env.sonarrDo(ctx, "GET", "/api/v3/series/lookup?term="+url.QueryEscape(s.Name), nil, &res); err != nil {
		return nil, err
	}
	for _, r := range res {
		if tm, _ := r["tmdbId"].(float64); int(tm) == s.ID && s.ID > 0 {
			return r, nil
		}
	}
	for _, r := range res {
		if y, _ := r["year"].(float64); int(y) == s.Year() {
			if t, _ := r["title"].(string); strings.EqualFold(t, s.Name) {
				return r, nil
			}
		}
	}
	return nil, fmt.Errorf("Sonarr couldn't find %s in its show database (TheTVDB) — it may be too new or not listed there yet", s.Name)
}

// waitForEpisodes polls until Sonarr has listed the new series' episodes and
// finished its first refresh (which is when it applies its own add options).
func (env *Env) waitForEpisodes(ctx context.Context, id int) ([]SonarrEpisode, error) {
	deadline := time.Now().Add(150 * time.Second)
	settled := 0
	for time.Now().Before(deadline) {
		eps, err := env.SonarrEpisodes(ctx, id)
		if err == nil && len(eps) > 0 && !env.refreshRunning(ctx, id) {
			settled++
			if settled >= 2 {
				return env.SonarrEpisodes(ctx, id)
			}
		}
		select {
		case <-time.After(waitPoll):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("Sonarr hasn't listed the episodes yet — choose them from the details screen in a minute")
}

// waitPoll is how often to ask Sonarr whether the new show is ready (shorter in tests).
var waitPoll = 1500 * time.Millisecond

func (env *Env) refreshRunning(ctx context.Context, id int) bool {
	var cmds []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Body   struct {
			SeriesID  int   `json:"seriesId"`
			SeriesIDs []int `json:"seriesIds"`
		} `json:"body"`
	}
	if env.sonarrDo(ctx, "GET", "/api/v3/command", nil, &cmds) != nil {
		return false
	}
	for _, c := range cmds {
		if c.Name != "RefreshSeries" || (c.Status != "queued" && c.Status != "started") {
			continue
		}
		if c.Body.SeriesID == id || contains(c.Body.SeriesIDs, id) || (c.Body.SeriesID == 0 && len(c.Body.SeriesIDs) == 0) {
			return true
		}
	}
	return false
}

func contains[T comparable](xs []T, v T) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
