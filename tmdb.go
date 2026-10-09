package main

// TMDB client for TV: the channel listings (discover), show details, seasons,
// genres and streaming services. "This product uses the TMDB API but is not
// endorsed or certified by TMDB." Streaming availability comes from JustWatch.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (env *Env) tmdbBase() string {
	if env.TMDBBase != "" {
		return env.TMDBBase
	}
	return "https://api.themoviedb.org/3"
}

func (env *Env) imageBase() string {
	if env.ImageBase != "" {
		return env.ImageBase
	}
	return "https://image.tmdb.org/t/p/"
}

// ImageURL builds a TMDB image URL ("w185", "/abc.jpg").
func (env *Env) ImageURL(size, path string) string {
	if path == "" {
		return ""
	}
	return env.imageBase() + size + path
}

// APIError is a problem the user can fix (bad key, rate limit) rather than a network blip.
type APIError struct {
	Service string
	Status  int
	Msg     string
}

func (e *APIError) Error() string { return e.Msg }

var errNoKey = errors.New("no TMDB key")

func isBearer(key string) bool { return len(key) > 60 && strings.Count(key, ".") == 2 }

func (env *Env) tmdbGet(ctx context.Context, path string, q url.Values, v any) error {
	key := env.Config.TMDBKey
	if key == "" {
		return errNoKey
	}
	if q == nil {
		q = url.Values{}
	}
	if !isBearer(key) {
		q.Set("api_key", key)
	}
	u := env.tmdbBase() + path + "?" + q.Encode()
	for attempt := 0; ; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		if isBearer(key) {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		t0 := time.Now()
		resp, err := env.HTTP.Do(req)
		if err != nil {
			err = redactKey(err, key)
			log.Printf("TMDB %s: attempt %d failed after %s: %v", path, attempt+1, time.Since(t0).Round(time.Millisecond), err)
			if attempt < 2 && ctx.Err() == nil {
				env.HTTP.CloseIdleConnections() // a stalled connection shouldn't be reused
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return err
		}
		if d := time.Since(t0); d > 5*time.Second {
			log.Printf("TMDB %s: slow answer (%s)", path, d.Round(time.Millisecond))
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		switch {
		case resp.StatusCode == 200:
			if err := json.Unmarshal(body, v); err != nil {
				log.Printf("TMDB %s: can't read the answer (%v): %.300s", path, err, body)
				return err
			}
			return nil
		case resp.StatusCode == 429 || resp.StatusCode >= 500:
			if attempt < 3 {
				wait := time.Duration(attempt+1) * 2 * time.Second
				if s, _ := strconv.Atoi(resp.Header.Get("Retry-After")); s > 0 && s < 30 {
					wait = time.Duration(s) * time.Second
				}
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			return &APIError{"TMDB", resp.StatusCode, "TMDB is busy (" + resp.Status + ") — try again in a minute"}
		case resp.StatusCode == 401:
			where := "tmdb_api_key in App/Cablebox/data/config.json"
			if env.Config.Borrowed {
				where = "the TMDB key borrowed from Rewind (App/Rewind/data/config.json)"
			}
			return &APIError{"TMDB", 401, "TMDB rejected the key — check " + where}
		case resp.StatusCode == 404:
			return &APIError{"TMDB", 404, "not found on TMDB"}
		default:
			var e struct {
				Msg string `json:"status_message"`
			}
			json.Unmarshal(body, &e)
			if e.Msg == "" {
				e.Msg = resp.Status
			}
			return &APIError{"TMDB", resp.StatusCode, "TMDB: " + e.Msg}
		}
	}
}

type discoverTVPage struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
	Results    []struct {
		ID         int      `json:"id"`
		Name       string   `json:"name"`
		FirstAir   string   `json:"first_air_date"`
		Poster     string   `json:"poster_path"`
		Backdrop   string   `json:"backdrop_path"`
		Overview   string   `json:"overview"`
		GenreIDs   []int    `json:"genre_ids"`
		Popularity float64  `json:"popularity"`
		VoteAvg    float64  `json:"vote_average"`
		VoteCount  int      `json:"vote_count"`
		Lang       string   `json:"original_language"`
		Countries  []string `json:"origin_country"`
	} `json:"results"`
}

// channelQuery builds the discover query for a channel.
// plain drops the optional filters, as a fallback when TMDB rejects or empties a listing.
func (env *Env) channelQuery(ch *Channel, page int, plain bool) url.Values {
	cfg := env.Config
	now := env.Now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	q := url.Values{}
	q.Set("language", cfg.Language)
	q.Set("watch_region", cfg.Region)
	q.Set("sort_by", "popularity.desc")
	q.Set("page", strconv.Itoa(page))
	if !plain {
		q.Set("with_watch_monetization_types", "flatrate|free|ads")
		q.Set("include_null_first_air_dates", "false")
		// keep talk shows, news and soaps out of the general channels
		q.Set("without_genres", "10767|10763|10766")
	}
	switch ch.Kind {
	case chNew:
		q.Set("first_air_date.gte", day(-120))
		q.Set("first_air_date.lte", day(1))
	case chAiring:
		q.Set("air_date.gte", day(-7))
		q.Set("air_date.lte", day(1))
	case chTop:
		q.Set("sort_by", "vote_average.desc")
		q.Set("vote_count.gte", "400")
	case chService:
		q.Set("with_watch_providers", ch.Providers)
	case chGenre:
		q.Set("with_genres", strconv.Itoa(ch.Genre))
		if !plain {
			q.Set("without_genres", strings.Trim(strings.ReplaceAll("|10767|10763|10766|", "|"+strconv.Itoa(ch.Genre)+"|", "|"), "|"))
		}
	}
	return q
}

// Discover lists a channel's shows, most popular first. If TMDB refuses the
// full query or returns nothing, it tries again without the optional filters.
func (env *Env) Discover(ctx context.Context, ch *Channel, max int, onPage func([]*Show)) ([]*Show, error) {
	if ch.Kind == chSearch {
		return env.Search(ctx, ch.Query, max, onPage)
	}
	out, err := env.discover(ctx, ch, max, false, onPage)
	if (err == nil && len(out) == 0) || retryPlain(err) {
		log.Printf("CH %02d %s: %d shows, err=%v — retrying without the extra filters", ch.Num, ch.Name, len(out), err)
		if out2, err2 := env.discover(ctx, ch, max, true, onPage); err2 == nil && len(out2) > 0 {
			return out2, nil
		} else if err == nil {
			err = err2
		}
	}
	return out, err
}

// retryPlain: worth trying the simpler query (not for a bad key, rate limit or no network).
func retryPlain(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		return errors.As(err, &se) || errors.As(err, &te)
	}
	return ae.Status != 401 && ae.Status != 429 && ae.Status < 500
}

func (env *Env) discover(ctx context.Context, ch *Channel, max int, plain bool, onPage func([]*Show)) ([]*Show, error) {
	label := fmt.Sprintf("CH %02d %s", ch.Num, ch.Name)
	return env.pages(ctx, label, "/discover/tv", func(page int) url.Values { return env.channelQuery(ch, page, plain) }, max, onPage)
}

// Search finds shows by name.
func (env *Env) Search(ctx context.Context, query string, max int, onPage func([]*Show)) ([]*Show, error) {
	return env.pages(ctx, "SEARCH "+strconv.Quote(query), "/search/tv", func(page int) url.Values {
		return url.Values{"query": {query}, "language": {env.Config.Language}, "include_adult": {"false"}, "page": {strconv.Itoa(page)}}
	}, max, onPage)
}

// pages reads up to max shows from a paged TMDB list (discover or search).
func (env *Env) pages(ctx context.Context, label, path string, query func(page int) url.Values, max int, onPage func([]*Show)) ([]*Show, error) {
	var out []*Show
	seen := map[int]bool{}
	for page := 1; page <= 5 && len(out) < max; page++ {
		var dp discoverTVPage
		q := query(page)
		t0 := time.Now()
		err := env.tmdbGet(ctx, path, q, &dp)
		q.Del("api_key")
		log.Printf("%s page %d: %d results in %s, err=%v [%s]", label, page, len(dp.Results), time.Since(t0).Round(time.Millisecond), err, q.Encode())
		if err != nil {
			if len(out) > 0 {
				break
			}
			return nil, err
		}
		for _, r := range dp.Results {
			if seen[r.ID] || r.Name == "" {
				continue
			}
			seen[r.ID] = true
			s := &Show{ID: r.ID, Name: r.Name, Poster: r.Poster, Backdrop: r.Backdrop, Overview: r.Overview,
				GenreIDs: r.GenreIDs, Popularity: r.Popularity, VoteAvg: r.VoteAvg, VoteCount: r.VoteCount,
				FirstAir: r.FirstAir, Lang: r.Lang, RT: -1, Meta: -1}
			out = append(out, s)
			if len(out) >= max {
				break
			}
		}
		if onPage != nil && len(out) > 0 {
			onPage(append([]*Show(nil), out...))
		}
		if page >= dp.TotalPages {
			break
		}
	}
	return out, nil
}

// Genres returns TMDB's id → name table for TV.
func (env *Env) Genres(ctx context.Context) (map[int]string, error) {
	var g struct {
		Genres []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"genres"`
	}
	q := url.Values{"language": {env.Config.Language}}
	if err := env.tmdbGet(ctx, "/genre/tv/list", q, &g); err != nil {
		return nil, err
	}
	m := map[int]string{}
	for _, x := range g.Genres {
		m[x.ID] = x.Name
	}
	return m, nil
}

type ServiceEntry struct {
	ID       int    `json:"provider_id"`
	Name     string `json:"provider_name"`
	Priority int    `json:"display_priority"`
	Logo     string `json:"logo_path"`
}

// Services lists the streaming services TMDB knows for TV in the region.
func (env *Env) Services(ctx context.Context) ([]ServiceEntry, error) {
	var r struct {
		Results []struct {
			ServiceEntry
			Priorities map[string]int `json:"display_priorities"`
		} `json:"results"`
	}
	q := url.Values{"watch_region": {env.Config.Region}, "language": {env.Config.Language}}
	if err := env.tmdbGet(ctx, "/watch/providers/tv", q, &r); err != nil {
		return nil, err
	}
	var out []ServiceEntry
	for _, x := range r.Results {
		e := x.ServiceEntry
		if p, ok := x.Priorities[env.Config.Region]; ok {
			e.Priority = p
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out, nil
}

// matchService finds every TMDB provider id belonging to a service name
// ("Paramount+" covers Paramount Plus, Paramount+ Essential, Premium…).
func matchService(name string, all []ServiceEntry) []int {
	want := serviceKey(name)
	var ids []int
	for _, e := range all {
		k := serviceKey(cleanProvider(e.Name))
		if k == want || strings.HasPrefix(serviceKey(e.Name), want) {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func serviceKey(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("+", "plus", " ", "", "-", "", ".", "", "'", "").Replace(s)
	s = strings.TrimPrefix(s, "amazon")
	return s
}

type tmdbShow struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Tagline  string `json:"tagline"`
	Overview string `json:"overview"`
	Poster   string `json:"poster_path"`
	Backdrop string `json:"backdrop_path"`
	FirstAir string `json:"first_air_date"`
	LastAir  string `json:"last_air_date"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Lang     string `json:"original_language"`
	Runtimes []int  `json:"episode_run_time"`
	Genres   []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Networks []struct {
		Name string `json:"name"`
	} `json:"networks"`
	CreatedBy []struct {
		Name string `json:"name"`
	} `json:"created_by"`
	Popularity  float64       `json:"popularity"`
	VoteAvg     float64       `json:"vote_average"`
	VoteCount   int           `json:"vote_count"`
	NumSeasons  int           `json:"number_of_seasons"`
	NumEpisodes int           `json:"number_of_episodes"`
	NextEp      *tmdbEpisode  `json:"next_episode_to_air"`
	LastEp      *tmdbEpisode  `json:"last_episode_to_air"`
	Seasons     []tmdbSeason  `json:"seasons"`
	External    tmdbExternals `json:"external_ids"`
	Ratings     struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	Credits struct {
		Cast []struct {
			Name      string `json:"name"`
			Character string `json:"character"`
			Order     int    `json:"order"`
		} `json:"cast"`
	} `json:"credits"` // the current cast; aggregate_credits can run to megabytes for long shows
	Videos struct {
		Results []tmdbVideo `json:"results"`
	} `json:"videos"`
	Providers struct {
		Results map[string]struct {
			Link     string         `json:"link"`
			Flatrate []tmdbProvider `json:"flatrate"`
			Free     []tmdbProvider `json:"free"`
			Ads      []tmdbProvider `json:"ads"`
		} `json:"results"`
	} `json:"watch/providers"`
}

type tmdbExternals struct {
	TVDB int    `json:"tvdb_id"`
	IMDb string `json:"imdb_id"`
}

type tmdbEpisode struct {
	Season   int     `json:"season_number"`
	Number   int     `json:"episode_number"`
	Name     string  `json:"name"`
	AirDate  string  `json:"air_date"`
	Runtime  int     `json:"runtime"`
	VoteAvg  float64 `json:"vote_average"`
	Overview string  `json:"overview"`
}

type tmdbSeason struct {
	Number   int    `json:"season_number"`
	Name     string `json:"name"`
	Episodes int    `json:"episode_count"`
	AirDate  string `json:"air_date"`
}

type tmdbVideo struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Site      string `json:"site"`
	Type      string `json:"type"`
	Official  bool   `json:"official"`
	Lang      string `json:"iso_639_1"`
	Published string `json:"published_at"`
}

type tmdbProvider struct {
	ID       int    `json:"provider_id"`
	Name     string `json:"provider_name"`
	Priority int    `json:"display_priority"`
}

// Details fills in seasons, rating, cast, services, trailer and the TVDB id Sonarr needs.
func (env *Env) Details(ctx context.Context, s *Show) error {
	q := url.Values{}
	q.Set("language", env.Config.Language)
	q.Set("append_to_response", "credits,videos,content_ratings,watch/providers,external_ids")
	lang := strings.SplitN(env.Config.Language, "-", 2)[0]
	q.Set("include_video_language", lang+",en,null")
	var d tmdbShow
	if err := env.tmdbGet(ctx, "/tv/"+strconv.Itoa(s.ID), q, &d); err != nil {
		return err
	}
	applyDetails(s, &d, env.Config.Region)
	s.DetailAt = env.Now()
	return nil
}

func applyDetails(s *Show, d *tmdbShow, region string) {
	if d.Name != "" {
		s.Name = d.Name
	}
	s.Tagline = strings.TrimSpace(d.Tagline)
	if d.Overview != "" {
		s.Overview = d.Overview
	}
	if d.Poster != "" {
		s.Poster = d.Poster
	}
	if d.Backdrop != "" {
		s.Backdrop = d.Backdrop
	}
	s.FirstAir, s.LastAir = d.FirstAir, d.LastAir
	s.Status, s.Type, s.Lang = d.Status, d.Type, d.Lang
	if d.Popularity > 0 {
		s.Popularity = d.Popularity
	}
	s.VoteAvg, s.VoteCount = d.VoteAvg, d.VoteCount
	s.GenreIDs, s.Genres = s.GenreIDs[:0], s.Genres[:0]
	for _, g := range d.Genres {
		s.GenreIDs = append(s.GenreIDs, g.ID)
		s.Genres = append(s.Genres, g.Name)
	}
	s.Networks = s.Networks[:0]
	for _, n := range d.Networks {
		s.Networks = append(s.Networks, n.Name)
	}
	s.Creators = s.Creators[:0]
	for _, c := range d.CreatedBy {
		s.Creators = append(s.Creators, c.Name)
	}
	s.Runtime = 0
	if len(d.Runtimes) > 0 {
		s.Runtime = d.Runtimes[0]
	} else if d.LastEp != nil {
		s.Runtime = d.LastEp.Runtime
	}
	s.NumSeasons, s.NumEpisodes = d.NumSeasons, d.NumEpisodes
	s.Seasons = s.Seasons[:0]
	for _, x := range d.Seasons {
		s.Seasons = append(s.Seasons, SeasonInfo{Number: x.Number, Name: x.Name, Episodes: x.Episodes, AirDate: x.AirDate})
	}
	s.NextEp, s.LastEp = epRef(d.NextEp), epRef(d.LastEp)
	s.TVDBID, s.IMDbID = d.External.TVDB, d.External.IMDb
	s.Cert = ""
	for _, r := range d.Ratings.Results {
		if r.Country == region && strings.TrimSpace(r.Rating) != "" {
			s.Cert = strings.TrimSpace(r.Rating)
		}
	}
	if s.Cert == "" {
		for _, r := range d.Ratings.Results {
			if r.Country == "US" && r.Rating != "" {
				s.Cert = r.Rating
			}
		}
	}
	cast := d.Credits.Cast
	sort.SliceStable(cast, func(i, j int) bool { return cast[i].Order < cast[j].Order })
	s.Cast = s.Cast[:0]
	for i, c := range cast {
		if i == 10 {
			break
		}
		s.Cast = append(s.Cast, CastMember{Name: c.Name, Role: c.Character})
	}
	s.Trailer = pickTrailer(d.Videos.Results)
	s.Providers = s.Providers[:0]
	s.WatchLink = ""
	if r, ok := d.Providers.Results[region]; ok {
		s.WatchLink = r.Link
		seen := map[string]bool{}
		for _, list := range [][]tmdbProvider{r.Flatrate, r.Free, r.Ads} {
			sort.SliceStable(list, func(i, j int) bool { return list[i].Priority < list[j].Priority })
			for _, p := range list {
				name := cleanProvider(p.Name)
				if name != "" && !seen[name] {
					seen[name] = true
					s.Providers = append(s.Providers, name)
				}
			}
		}
	}
}

func epRef(e *tmdbEpisode) *EpisodeRef {
	if e == nil || e.Season == 0 && e.Number == 0 {
		return nil
	}
	return &EpisodeRef{Season: e.Season, Number: e.Number, Name: e.Name, AirDate: e.AirDate}
}

// Season lists one season's episodes (for picking episodes to order).
func (env *Env) Season(ctx context.Context, showID, n int) ([]Episode, error) {
	var r struct {
		Episodes []tmdbEpisode `json:"episodes"`
	}
	q := url.Values{"language": {env.Config.Language}}
	if err := env.tmdbGet(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, n), q, &r); err != nil {
		return nil, err
	}
	var out []Episode
	for _, e := range r.Episodes {
		out = append(out, Episode{Season: e.Season, Number: e.Number, Name: e.Name, AirDate: e.AirDate, Runtime: e.Runtime})
	}
	return out, nil
}

// pickTrailer prefers an official YouTube trailer in English, newest first.
func pickTrailer(vs []tmdbVideo) string {
	score := func(v tmdbVideo) int {
		s := 0
		switch v.Type {
		case "Trailer":
			s += 100
		case "Teaser":
			s += 50
		case "Opening Credits", "Clip":
			s += 10
		default:
			return -1
		}
		if v.Official {
			s += 20
		}
		if v.Lang == "en" {
			s += 10
		}
		return s
	}
	best, bs := "", -1
	bestPub := ""
	for _, v := range vs {
		if v.Site != "YouTube" || v.Key == "" {
			continue
		}
		s := score(v)
		if s < 0 {
			continue
		}
		if s > bs || (s == bs && v.Published > bestPub) {
			best, bs, bestPub = v.Key, s, v.Published
		}
	}
	return best
}

// cleanProvider shortens JustWatch's long service names for the small screen.
func cleanProvider(s string) string {
	r := strings.NewReplacer(
		"Amazon Prime Video with Ads", "Prime Video",
		"Amazon Prime Video", "Prime Video",
		"Netflix Standard with Ads", "Netflix",
		"Netflix basic with Ads", "Netflix",
		"Disney Plus", "Disney+",
		"Apple TV Plus", "Apple TV",
		"Apple TV+", "Apple TV",
		"Paramount Plus Premium", "Paramount+",
		"Paramount Plus Essential", "Paramount+",
		"Paramount+ Premium", "Paramount+",
		"Paramount+ Essential", "Paramount+",
		"Paramount Plus", "Paramount+",
		"Peacock Premium Plus", "Peacock",
		"Peacock Premium", "Peacock",
		"HBO Max Amazon Channel", "HBO Max",
	)
	s = r.Replace(s)
	for _, suf := range []string{" Amazon Channel", " Apple TV Channel", " Roku Premium Channel", " Standard with Ads", " with Ads"} {
		s = strings.TrimSuffix(s, suf)
	}
	if s == "Max" {
		s = "HBO Max"
	}
	return strings.TrimSpace(s)
}

func netHint(err error) error {
	var ae *APIError
	if errors.As(err, &ae) {
		return err
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "certificate has expired") || strings.Contains(s, "not yet valid"):
		return fmt.Errorf("secure connection failed — the clock looks wrong. Sync the time in Tweaks → System → Date and time")
	case strings.Contains(s, "connection refused"):
		return fmt.Errorf("the server is up but nothing answered on that port — is the address right?")
	case strings.Contains(s, "no route to host") || strings.Contains(s, "host is down"):
		return fmt.Errorf("can't reach the server — is the Miyoo on the same network?")
	case strings.Contains(s, "no such host") || strings.Contains(s, "network is unreachable") || strings.Contains(s, "dial"):
		return fmt.Errorf("no network — turn Wi-Fi on in Settings → Network")
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return fmt.Errorf("the connection timed out — check Wi-Fi and try again")
	}
	return err
}

// redactKey keeps the API key out of error messages and the log.
func redactKey(err error, key string) error {
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return &redacted{err, strings.ReplaceAll(err.Error(), key, "***")}
}

type redacted struct {
	err error
	msg string
}

func (r *redacted) Error() string { return r.msg }
func (r *redacted) Unwrap() error { return r.err }
