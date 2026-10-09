package main

// The guide: shows, the channel lineup, and the cache on the SD card.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CastMember struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

type SeasonInfo struct {
	Number   int    `json:"n"`
	Name     string `json:"name,omitempty"`
	Episodes int    `json:"eps"`
	AirDate  string `json:"air,omitempty"`
}

type EpisodeRef struct {
	Season  int    `json:"s"`
	Number  int    `json:"e"`
	Name    string `json:"name,omitempty"`
	AirDate string `json:"air,omitempty"`
}

// Episode is one entry in the episode picker.
type Episode struct {
	Season  int
	Number  int
	Name    string
	AirDate string
	Runtime int
	// from Sonarr, for shows already ordered
	SonarrID  int
	Monitored bool
	HasFile   bool
}

type Show struct {
	ID          int          `json:"id"`
	Name        string       `json:"name"`
	Tagline     string       `json:"tagline,omitempty"`
	Overview    string       `json:"overview,omitempty"`
	Poster      string       `json:"poster,omitempty"`
	Backdrop    string       `json:"backdrop,omitempty"`
	FirstAir    string       `json:"first_air,omitempty"`
	LastAir     string       `json:"last_air,omitempty"`
	Status      string       `json:"status,omitempty"` // Returning Series, Ended, Canceled, In Production…
	Type        string       `json:"type,omitempty"`   // Scripted, Reality, Documentary, Miniseries…
	Lang        string       `json:"lang,omitempty"`
	GenreIDs    []int        `json:"genre_ids,omitempty"`
	Genres      []string     `json:"genres,omitempty"`
	Networks    []string     `json:"networks,omitempty"`
	Creators    []string     `json:"creators,omitempty"`
	Cast        []CastMember `json:"cast,omitempty"`
	Runtime     int          `json:"runtime,omitempty"`
	NumSeasons  int          `json:"seasons_n,omitempty"`
	NumEpisodes int          `json:"episodes_n,omitempty"`
	Seasons     []SeasonInfo `json:"seasons,omitempty"`
	NextEp      *EpisodeRef  `json:"next_ep,omitempty"`
	LastEp      *EpisodeRef  `json:"last_ep,omitempty"`
	Cert        string       `json:"cert,omitempty"`
	Providers   []string     `json:"providers,omitempty"`
	WatchLink   string       `json:"watch_link,omitempty"`
	Trailer     string       `json:"trailer,omitempty"` // YouTube key
	TVDBID      int          `json:"tvdb,omitempty"`
	IMDbID      string       `json:"imdb_id,omitempty"`
	Popularity  float64      `json:"pop,omitempty"`
	VoteAvg     float64      `json:"vote,omitempty"`
	VoteCount   int          `json:"votes,omitempty"`
	IMDb        float64      `json:"imdb,omitempty"`
	IMDbVotes   string       `json:"imdb_votes,omitempty"`
	RT          int          `json:"rt"`
	Meta        int          `json:"meta"`
	Awards      string       `json:"awards,omitempty"`
	DetailAt    time.Time    `json:"detail_at,omitzero"`
	ScoresAt    time.Time    `json:"scores_at,omitzero"`
}

func (s *Show) Year() int {
	if len(s.FirstAir) >= 4 {
		y, _ := strconv.Atoi(s.FirstAir[:4])
		return y
	}
	return 0
}

func (s *Show) Ended() bool { return s.Status == "Ended" || s.Status == "Canceled" }

// Years reads like 2019–, 2016–2022 or 2024.
func (s *Show) Years() string {
	y := s.Year()
	if y == 0 {
		return ""
	}
	if s.Ended() {
		if len(s.LastAir) >= 4 && s.LastAir[:4] != s.FirstAir[:4] {
			return fmt.Sprintf("%d–%s", y, s.LastAir[:4])
		}
		return strconv.Itoa(y)
	}
	if s.DetailAt.IsZero() {
		return strconv.Itoa(y)
	}
	return fmt.Sprintf("%d–", y)
}

// StatusWord is the guide's one-word status.
func (s *Show) StatusWord() string {
	switch s.Status {
	case "Returning Series":
		return "RETURNING"
	case "Ended":
		return "ENDED"
	case "Canceled":
		return "CANCELED"
	case "In Production":
		return "IN PRODUCTION"
	case "Planned":
		return "PLANNED"
	case "Pilot":
		return "PILOT"
	}
	return strings.ToUpper(s.Status)
}

// RealSeasons are the numbered seasons (no specials).
func (s *Show) RealSeasons() []SeasonInfo {
	var out []SeasonInfo
	for _, x := range s.Seasons {
		if x.Number > 0 {
			out = append(out, x)
		}
	}
	return out
}

// MetaLine: 2019– · 4 SEASONS · TV-MA · 50M
func (s *Show) MetaLine() string {
	var parts []string
	if y := s.Years(); y != "" {
		parts = append(parts, y)
	}
	if s.NumSeasons > 0 {
		parts = append(parts, fmt.Sprintf("%d SEASON%s", s.NumSeasons, plural(s.NumSeasons)))
	}
	if s.Cert != "" {
		parts = append(parts, s.Cert)
	}
	if s.Runtime > 0 {
		parts = append(parts, fmt.Sprintf("%dM", s.Runtime))
	}
	return strings.Join(parts, " · ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "S"
}

// ---------- channels ----------

type chanKind int

const (
	chNew chanKind = iota
	chAiring
	chPopular
	chTop
	chService
	chGenre
	chSearch
)

type Channel struct {
	Num       int
	Key       string
	Name      string // GUIDE LISTING NAME
	Kind      chanKind
	Providers string // TMDB provider ids, "8|1796"
	Genre     int
	Group     string // NOW · SERVICES · GENRES
	Query     string // the search channel's current search
}

var genreOrder = []struct {
	id   int
	name string
}{
	{18, "Drama"}, {35, "Comedy"}, {80, "Crime"}, {10765, "Sci-Fi & Fantasy"}, {10759, "Action & Adventure"},
	{9648, "Mystery"}, {16, "Animation"}, {99, "Documentary"}, {10764, "Reality"}, {10751, "Family"},
	{10762, "Kids"}, {10768, "War & Politics"}, {37, "Western"},
}

// Lineup builds the channel list: what's on now, one channel per service, one per genre.
func Lineup(services []string, all []ServiceEntry, genres map[int]string) []*Channel {
	chs := []*Channel{
		{Num: 1, Key: "search", Name: "SEARCH", Kind: chSearch, Group: "NOW"},
		{Num: 2, Key: "new", Name: "NEW SERIES", Kind: chNew, Group: "NOW"},
		{Num: 3, Key: "airing", Name: "ON THIS WEEK", Kind: chAiring, Group: "NOW"},
		{Num: 4, Key: "popular", Name: "POPULAR", Kind: chPopular, Group: "NOW"},
		{Num: 5, Key: "top", Name: "TOP RATED", Kind: chTop, Group: "NOW"},
	}
	n := 10
	for _, name := range services {
		ids := matchService(name, all)
		if len(ids) == 0 && len(all) > 0 {
			continue // not offered in this region
		}
		var parts []string
		for _, id := range ids {
			parts = append(parts, strconv.Itoa(id))
		}
		chs = append(chs, &Channel{Num: n, Key: "svc-" + serviceKey(name), Name: strings.ToUpper(name), Kind: chService,
			Providers: strings.Join(parts, "|"), Group: "SERVICES"})
		n++
	}
	n = max(n+1, 30)
	for _, g := range genreOrder {
		name := g.name
		if v, ok := genres[g.id]; ok && v != "" {
			name = v
		}
		chs = append(chs, &Channel{Num: n, Key: "genre-" + strconv.Itoa(g.id), Name: strings.ToUpper(name), Kind: chGenre, Genre: g.id, Group: "GENRES"})
		n++
	}
	return chs
}

// ---------- the cache ----------

type Listing struct {
	IDs []int     `json:"ids"`
	At  time.Time `json:"at"`
}

type Guide struct {
	Shows      map[int]*Show       `json:"shows"`
	Lists      map[string]*Listing `json:"lists"`
	Genres     map[int]string      `json:"genres"`
	Services   []ServiceEntry      `json:"services"`
	ServicesAt time.Time           `json:"services_at,omitzero"`
	Channel    int                 `json:"channel"` // last channel watched
	Sort       int                 `json:"sort"`
	Search     string              `json:"search,omitempty"` // what CH 01 is showing
	Recent     []string            `json:"recent,omitempty"` // recent searches, newest first
}

// AddRecent remembers a search, newest first, without repeats.
func (g *Guide) AddRecent(q string) {
	out := []string{q}
	for _, r := range g.Recent {
		if !strings.EqualFold(r, q) && len(out) < 5 {
			out = append(out, r)
		}
	}
	g.Recent = out
}

func newGuide() *Guide {
	return &Guide{Shows: map[int]*Show{}, Lists: map[string]*Listing{}, Genres: map[int]string{}}
}

func (env *Env) guidePath() string { return filepath.Join(env.DataDir, "guide.json") }

func (env *Env) LoadGuide() *Guide {
	g := newGuide()
	b, err := os.ReadFile(env.guidePath())
	if err != nil || json.Unmarshal(b, g) != nil {
		return newGuide()
	}
	if g.Shows == nil {
		g.Shows = map[int]*Show{}
	}
	if g.Lists == nil {
		g.Lists = map[string]*Listing{}
	}
	if g.Genres == nil {
		g.Genres = map[int]string{}
	}
	return g
}

// SaveGuide writes the cache, dropping shows no channel lists any more.
func (env *Env) SaveGuide(g *Guide) {
	keep := map[int]bool{}
	for _, l := range g.Lists {
		for _, id := range l.IDs {
			keep[id] = true
		}
	}
	for id := range g.Shows {
		if !keep[id] {
			delete(g.Shows, id)
		}
	}
	b, err := json.Marshal(g)
	if err != nil {
		return
	}
	tmp := env.guidePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, env.guidePath())
	}
}

// Shows returns a channel's shows in listing order.
func (g *Guide) ChannelShows(key string) []*Show {
	l := g.Lists[key]
	if l == nil {
		return nil
	}
	var out []*Show
	for _, id := range l.IDs {
		if s := g.Shows[id]; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// Merge stores a fresh listing, keeping details already fetched for known shows.
func (g *Guide) Merge(key string, shows []*Show, at time.Time) {
	l := &Listing{At: at}
	for _, s := range shows {
		if old := g.Shows[s.ID]; old != nil {
			old.Popularity = s.Popularity
			if old.DetailAt.IsZero() {
				old.Overview, old.Poster, old.VoteAvg, old.VoteCount = s.Overview, s.Poster, s.VoteAvg, s.VoteCount
			}
		} else {
			g.Shows[s.ID] = s
		}
		l.IDs = append(l.IDs, s.ID)
	}
	g.Lists[key] = l
}

// ---------- sorting ----------

var sortNames = []string{"CHANNEL ORDER", "IMDB RATING", "NEWEST", "A–Z"}

func sortShows(ss []*Show, by int) {
	switch by {
	case 1:
		sort.SliceStable(ss, func(i, j int) bool { return rating(ss[i]) > rating(ss[j]) })
	case 2:
		sort.SliceStable(ss, func(i, j int) bool { return ss[i].FirstAir > ss[j].FirstAir })
	case 3:
		sort.SliceStable(ss, func(i, j int) bool { return strings.ToLower(ss[i].Name) < strings.ToLower(ss[j].Name) })
	}
}

// rating is IMDb's when known, TMDB's otherwise (TMDB with few votes counts for less).
func rating(s *Show) float64 {
	if s.IMDb > 0 {
		return s.IMDb
	}
	if s.VoteCount < 30 {
		return s.VoteAvg - 2
	}
	return s.VoteAvg
}
