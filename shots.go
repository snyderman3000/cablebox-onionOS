package main

// Demo scenes for screenshots and tests (go run . -shot guide). The shows are
// invented, with made-up ratings and generated artwork, so the screenshots
// never misstate a real show's numbers.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"
)

type demoShow struct {
	name, cert, tagline, status string
	year, seasons, eps, run     int
	imdb                        float64
	rt                          int
	genres, svc                 []string
	hue                         float64
}

var demoShows = []demoShow{
	{"Harbor Lights", "TV-MA", "Every port has a secret.", "Returning Series", 2023, 3, 24, 52, 8.4, 93, []string{"Crime", "Drama"}, []string{"Netflix"}, 0.58},
	{"The Night Shift Arcade", "TV-14", "", "Returning Series", 2025, 2, 16, 30, 7.9, -1, []string{"Comedy"}, []string{"Hulu", "Disney+"}, 0.86},
	{"Static Valley", "TV-MA", "", "Ended", 2019, 4, 36, 48, 8.1, 88, []string{"Sci-Fi & Fantasy", "Mystery"}, []string{"HBO Max"}, 0.36},
	{"Paper Lanterns", "TV-PG", "", "Returning Series", 2026, 1, 8, 44, 7.2, -1, []string{"Drama", "Family"}, []string{"Apple TV"}, 0.12},
	{"Route 9 Mysteries", "TV-14", "", "Returning Series", 2024, 2, 20, 42, 7.6, -1, []string{"Mystery"}, []string{"Peacock"}, 0.03},
	{"Grand Delta", "TV-MA", "", "Canceled", 2022, 2, 18, 55, 6.4, 51, []string{"Drama"}, []string{"Paramount+"}, 0.92},
	{"A Quiet Frequency", "TV-14", "Somebody is still broadcasting.", "Returning Series", 2026, 1, 10, 49, 8.6, 97, []string{"Sci-Fi & Fantasy"}, []string{"Prime Video"}, 0.64},
	{"Ironwood County", "TV-MA", "", "Returning Series", 2021, 5, 50, 58, 7.0, -1, []string{"Crime", "Western"}, []string{"Netflix"}, 0.27},
	{"Sunday Drive", "TV-G", "", "Ended", 2018, 3, 30, 24, 6.9, -1, []string{"Reality"}, []string{"Hulu"}, 0.08},
	{"Cold Open", "TV-14", "", "Returning Series", 2025, 2, 20, 22, 7.7, 85, []string{"Comedy"}, []string{"Netflix"}, 0.72},
}

func demoGuide() *Guide {
	g := newGuide()
	names := []string{"Jordan Vale", "Marisol Quint", "Theo Barrow", "Ada Okafor", "Wes Calder", "Nina Park", "Ruben Holt", "Ivy Marchetti"}
	var ids []int
	for i, d := range demoShows {
		s := &Show{ID: 8000 + i, Name: d.name, Cert: d.cert, Tagline: d.tagline, Status: d.status, Runtime: d.run,
			FirstAir: fmt.Sprintf("%d-03-14", d.year), LastAir: fmt.Sprintf("%d-11-02", d.year+d.seasons-1),
			NumSeasons: d.seasons, NumEpisodes: d.eps, IMDb: d.imdb, IMDbVotes: fmt.Sprintf("%d,%03d", 12+i*7, 400),
			RT: d.rt, Meta: -1, VoteAvg: d.imdb - 0.3, VoteCount: 400, Genres: d.genres, Providers: d.svc,
			Poster: "/demo" + string(rune('a'+i)) + ".jpg", DetailAt: time.Now(), ScoresAt: time.Now(),
			TVDBID: 70000 + i, IMDbID: "tt0000000", Trailer: "dQw4w9WgXcQ", Networks: []string{d.svc[0]},
			Creators: []string{names[(i+3)%len(names)]}}
		s.Overview = "When " + names[i%len(names)] + " takes over the night desk at a small-town radio station, the calls start coming in from people who swear they've heard tomorrow's news. Each season follows a new mystery on the dial — and the station's long-silent sister tower."
		for j := 0; j < 5; j++ {
			s.Cast = append(s.Cast, CastMember{Name: names[(i+j)%len(names)]})
		}
		per := d.eps / d.seasons
		for n := 1; n <= d.seasons; n++ {
			s.Seasons = append(s.Seasons, SeasonInfo{Number: n, Episodes: per, AirDate: fmt.Sprintf("%d-03-14", d.year+n-1)})
		}
		if d.status == "Returning Series" {
			s.NextEp = &EpisodeRef{Season: d.seasons, Number: 4, AirDate: time.Now().AddDate(0, 0, 6).Format("2006-01-02")}
		}
		g.Shows[s.ID] = s
		ids = append(ids, s.ID)
	}
	g.Lists["svc-netflix"] = &Listing{IDs: ids, At: time.Now()}
	g.Services = []ServiceEntry{{ID: 8, Name: "Netflix"}, {ID: 15, Name: "Hulu"}, {ID: 9, Name: "Amazon Prime Video"}, {ID: 337, Name: "Disney Plus"},
		{ID: 1899, Name: "HBO Max"}, {ID: 350, Name: "Apple TV"}, {ID: 531, Name: "Paramount Plus"}, {ID: 386, Name: "Peacock Premium"}}
	g.ServicesAt = time.Now()
	return g
}

func demoLibrary() *SonarrLibrary {
	lib := &SonarrLibrary{ByTVDB: map[int]*SonarrSeries{}, ByTMDB: map[int]*SonarrSeries{}}
	a := &SonarrSeries{ID: 1, TVDBID: 70002, Monitored: true, Stats: SonarrStats{EpisodeFileCount: 36, EpisodeCount: 36, TotalEpisodeCount: 36}}
	b := &SonarrSeries{ID: 2, TVDBID: 70006, Monitored: true, Stats: SonarrStats{EpisodeFileCount: 3, EpisodeCount: 10, TotalEpisodeCount: 10},
		Seasons: []SonarrSeason{{Number: 1, Monitored: true, Stats: &SonarrStats{EpisodeFileCount: 3, TotalEpisodeCount: 10}}}}
	lib.ByTVDB[a.TVDBID], lib.ByTVDB[b.TVDBID] = a, b
	return lib
}

func demoEpisodes(season, n int, today string) []Episode {
	titles := []string{"Dead Air", "The Night Desk", "Request Line", "Sign-Off", "Interference", "Call Letters", "Clear Channel", "Skywave", "Test Pattern", "Last Broadcast"}
	var out []Episode
	for i := 1; i <= n; i++ {
		d := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*(i-1)).Format("2006-01-02")
		out = append(out, Episode{Season: season, Number: i, Name: titles[(i-1)%len(titles)], AirDate: d, HasFile: i <= 3 && season == 1})
	}
	return out
}

func demoScene(u *UI, scene string) {
	u.env.Config.TMDBKey = "demo"
	u.env.Config.SonarrURL, u.env.Config.SonarrKey = "http://192.168.4.34:8989", "demo"
	u.guide = demoGuide()
	u.rebuildLineup()
	for i, ch := range u.lineup {
		if ch.Key == "svc-netflix" {
			u.chIdx = i
		}
	}
	u.shows = u.guide.ChannelShows("svc-netflix")
	u.lib = demoLibrary()
	u.target = &SonarrTarget{Profile: "HD-1080p", Root: "/media/TV"}
	u.scr = scrGuide
	u.frame = 60
	u.tuneAt = -100
	u.sel = 0
	for i, s := range u.shows {
		for _, big := range []bool{false, true} {
			w, h := u.posterBox(big)
			size := "w185"
			if big {
				size = "w342"
			}
			k := fmt.Sprintf("%s|%dx%d", u.env.ImageURL(size, s.Poster), w, h)
			u.imgs[k] = demoArt(u, s.Name, demoShows[i].hue, w, h, -1)
		}
	}
	s := u.shows[0]
	switch scene {
	case "guide":
	case "tune":
		u.tuneAt = u.frame - 10
		u.sel = 2
	case "loading":
		u.shows = nil
		u.chLoading["svc-netflix"] = true
	case "detail":
		u.openDetail(s)
	case "detail-owned":
		u.sel = 6
		u.openDetail(u.shows[6])
	case "lineup":
		u.scr = scrLineup
		u.lineupSel = u.chIdx + 3
	case "order":
		u.openOrder(s)
		u.ord.sel = 3
	case "manage":
		u.cur = u.shows[6]
		u.openOrder(u.shows[6])
	case "seasons":
		u.openOrder(s)
		u.openPick(orderSeasons)
		u.pick.chosenS[2], u.pick.chosenS[3] = true, true
		u.pick.sel = 1
	case "episodes":
		u.openOrder(s)
		u.openPick(orderEpisodes)
		today := time.Now().Format("2006-01-02")
		for _, x := range s.Seasons {
			u.pick.eps[x.Number] = demoEpisodes(x.Number, x.Episodes, today)
		}
		u.pick.tab = 2
		for _, n := range []int{1, 2, 5} {
			u.pick.chosenE[EpKey{3, n}] = true
		}
		u.pick.chosenE[EpKey{1, 4}] = true
		u.pick.sel = 4
	case "progress":
		u.openOrder(s)
		u.prog = progressState{title: "ORDER HARBOR LIGHTS", started: time.Now().Add(-9 * time.Second),
			steps: []string{"SENDING ORDER", "LOOKING UP HARBOR LIGHTS", "ADDING TO SONARR", "WAITING FOR SONARR TO LIST EPISODES"}}
		u.modal = modalProgress
	case "done":
		u.openOrder(s)
		u.prog = progressState{title: "ORDER HARBOR LIGHTS", done: true, started: time.Now(),
			steps: []string{"SENDING ORDER", "LOOKING UP HARBOR LIGHTS", "ADDING TO SONARR", "WAITING FOR SONARR TO LIST EPISODES", "CHOOSING EPISODES", "ASKING SONARR TO SEARCH"},
			res:   &OrderResult{Summary: "Added. Monitoring seasons 2, 3 (16 episodes). Sonarr is searching for them now."}}
		u.modal = modalProgress
	case "trailer":
		u.openDetail(s)
		u.modal = modalTrailer
	case "nosonarr":
		u.message("NO SONARR CONNECTED", colYellow, "To order shows, add your Sonarr to App/Cablebox/data/config.json:\n\n\"sonarr_url\": \"192.168.4.34:8989\",\n\"sonarr_api_key\": \"from Sonarr → Settings → General\"")
	case "setup":
		u.scr = scrSetup
	case "bye":
		u.scr = scrBye
		u.byeFrame = 6
	}
}

// demoArt paints a poster or still: a sunset gradient, sun, grid and title.
func demoArt(u *UI, title string, hue float64, w, h int, still int) *image.RGBA {
	c := NewCanvas(w, h)
	top := hsv(hue, 0.75, 0.22)
	mid := hsv(math.Mod(hue+0.08, 1), 0.85, 0.85)
	horizon := h * 58 / 100
	if still >= 0 {
		horizon = h*(45+still*6)/100 + 0
	}
	for y := 0; y < h; y++ {
		col := top
		if y < horizon {
			col = mix(top, mid, math.Pow(float64(y)/float64(horizon), 1.6))
		} else {
			col = mix(rgb(0x14, 0x06, 0x22), rgb(0x05, 0x02, 0x0a), float64(y-horizon)/float64(h-horizon))
		}
		c.Fill(0, y, w, 1, col)
	}
	// sun with slats
	sr := w / 4
	if still >= 0 {
		sr = h / 4
	}
	scx := w / 2
	if still >= 0 {
		scx = w * (30 + still*12) / 100
	}
	for y := -sr; y <= 0; y++ {
		if (y/max(1, sr/8))%2 == 0 && y < -sr/3 || y >= -sr/3 && ((-y)/3)%2 == 0 {
			hw := int(math.Sqrt(float64(sr*sr - y*y)))
			c.Fill(scx-hw, horizon+y, 2*hw, 1, mix(rgb(0xff, 0xd0, 0x40), rgb(0xff, 0x40, 0x80), float64(-y)/float64(sr)))
		}
	}
	// perspective grid
	gc := hsv(math.Mod(hue+0.5, 1), 0.7, 0.9)
	for i := -10; i <= 10; i++ {
		c.Line(w/2, horizon, w/2+i*w/5, h, mix(rgb(0x14, 0x06, 0x22), gc, 0.6))
	}
	for k := 1; k < 8; k++ {
		y := horizon + int(float64(h-horizon)*math.Pow(float64(k)/8, 2))
		c.Fill(0, y, w, 1, mix(rgb(0x14, 0x06, 0x22), gc, 0.5))
	}
	if still < 0 {
		f := loadFont(titleTTF, float64(w)/9)
		y := h / 10
		for _, l := range f.Wrap(title, w-12) {
			c.TextCenter(f, w/2+1, y+1, l, rgb(0, 0, 0))
			c.TextCenter(f, w/2, y, l, rgb(0xff, 0xf2, 0xd8))
			y += f.height
		}
		sf := loadFont(bodyTTF, float64(w)/14)
		c.TextCenter(sf, w/2, h-sf.height-6, "NEW SERIES", rgb(0xff, 0xf2, 0xd8))
	}
	return c.RGBA
}

func hsv(h, s, v float64) color.RGBA {
	i := math.Floor(h * 6)
	f := h*6 - i
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return rgb(uint8(r*255), uint8(g*255), uint8(b*255))
}

// appIcon: a woodgrain cable box with its red channel number.
func appIcon(n int) *image.RGBA {
	c := NewCanvas(n, n)
	for i := range c.Pix {
		c.Pix[i] = 0
	}
	m := n * 5 / 100
	x, w := m, n-2*m
	h := n * 50 / 100
	y := (n - h) / 2
	c.Woodgrain(x, y, w, h*28/100)
	c.VGrad(x, y+h*28/100, w, h-h*28/100, colFace, rgb(0x0a, 0x08, 0x08))
	c.Fill(x, y+h*28/100, w, 1, rgb(0x8a, 0x84, 0x78))
	bx, by, bw, bh := x+w*52/100, y+h*42/100, w*40/100, h*46/100
	bg := rgb(0x12, 0x02, 0x02)
	c.Fill(bx, by, bw, bh, bg)
	dw := bw * 34 / 100
	t := max(2, dw/5)
	c.Seg7(bx+bw*8/100, by+bh*14/100, dw, bh*72/100, t, "04", rgb(0xff, 0x26, 0x14), bg)
	// a row of channel buttons
	for i := 0; i < 4; i++ {
		c.Fill(x+w*8/100+i*w*10/100, y+h*58/100, w*7/100, h*20/100, rgb(0x8a, 0x84, 0x78))
	}
	for _, p := range [][2]int{{x, y}, {x + w - 1, y}, {x, y + h - 1}, {x + w - 1, y + h - 1}} {
		c.Pix[p[1]*c.Stride+p[0]*4+3] = 0
	}
	return c.RGBA
}
