package main

// Ordering a show: the order menu, the season and episode pickers, and the
// progress window while Sonarr does the work.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type orderItem struct {
	label string
	desc  string
	act   func()
}

type orderState struct {
	show   *Show
	series *SonarrSeries // already in Sonarr
	items  []orderItem
	sel    int
}

type pickState struct {
	mode     OrderMode // orderSeasons or orderEpisodes
	show     *Show
	series   *SonarrSeries
	seasons  []SeasonInfo
	chosenS  map[int]bool
	chosenE  map[EpKey]bool
	eps      map[int][]Episode
	epErr    map[int]string
	loadingS map[int]bool
	loading  int
	tab      int
	sel      int
	scroll   int
	before   []SonarrEpisode // Sonarr's episodes for a show already ordered
	err      string
}

type progressState struct {
	title   string
	steps   []string
	done    bool
	err     string
	res     *OrderResult
	started time.Time
}

func (u *UI) openOrder(s *Show) {
	if !u.env.SonarrOn() {
		u.message("NO SONARR CONNECTED", colYellow, "To order shows, add your Sonarr to App/Cablebox/data/config.json:\n\n\"sonarr_url\": \"192.168.4.34:8989\",\n\"sonarr_api_key\": \"from Sonarr → Settings → General\"")
		return
	}
	u.cur = s
	if s.DetailAt.IsZero() {
		u.showToast("STILL LOADING THIS SHOW'S DETAILS — TRY AGAIN IN A MOMENT")
		return
	}
	if u.lib == nil {
		if u.libErr != "" {
			u.message("SONARR UNREACHABLE", colRed, u.libErr)
			u.syncSonarr()
			return
		}
		u.showToast("CHECKING SONARR'S LIBRARY…")
		u.syncSonarr()
		return
	}
	if u.target == nil {
		u.loadTarget()
	}
	o := &u.ord
	*o = orderState{show: s, series: u.inSonarr(s)}
	seasons := s.RealSeasons()
	nEps := 0
	for _, x := range seasons {
		nEps += x.Episodes
	}
	search := u.env.Config.search()
	pick := func(mode OrderMode) func() {
		return func() { u.openPick(mode) }
	}
	run := func(mode OrderMode, title string) func() {
		return func() { u.runOrder(title, Order{Mode: mode}) }
	}
	name := strings.ToUpper(s.Name)
	future := orderItem{"NEW EPISODES ONLY", "Gets each new episode as it airs. Nothing already out.", run(orderFuture, "ORDER "+name)}
	if s.Ended() {
		future.desc = "This show has ended, so this only matters if it comes back."
	}
	everyDesc := fmt.Sprintf("All %d episodes, plus new ones.", nEps)
	if search {
		everyDesc += " Sonarr starts searching now."
	}
	every := orderItem{"EVERY EPISODE", everyDesc, run(orderAll, "ORDER "+name)}
	seasonsItem := orderItem{"CHOOSE SEASONS…", fmt.Sprintf("Pick from %d season%s.", len(seasons), strings.ToLower(plural(len(seasons)))), pick(orderSeasons)}
	epsItem := orderItem{"CHOOSE EPISODES…", "Pick single episodes, season by season.", pick(orderEpisodes)}
	if o.series != nil {
		every.label, every.act = "MONITOR EVERY EPISODE", run(orderAll, "UPDATE "+name)
		future.label, future.act = "NEW EPISODES ONLY", run(orderFuture, "UPDATE "+name)
		future.desc = "Stop wanting older episodes; keep getting new ones."
		seasonsItem.desc = "Change which seasons Sonarr wants."
		epsItem.desc = "Change which episodes Sonarr wants."
		o.items = []orderItem{seasonsItem, epsItem, future, every,
			{"SEARCH FOR MISSING", "Ask Sonarr to look again for monitored episodes it doesn't have.", run(orderSearch, "SEARCH "+name)}}
	} else {
		o.items = []orderItem{future, every, seasonsItem, epsItem}
	}
	u.modal = modalOrder
}

func (u *UI) loadTarget() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		t, err := u.env.SonarrTarget(ctx)
		u.post <- func() {
			if err != nil {
				u.targetErr = err.Error()
				return
			}
			u.target, u.targetErr = t, ""
		}
	}()
}

func (u *UI) keyOrder(k int) {
	o := &u.ord
	n := len(o.items)
	switch k {
	case BtnUp:
		o.sel = (o.sel - 1 + n) % n
	case BtnDown:
		o.sel = (o.sel + 1) % n
	case BtnA:
		u.modal = modalNone
		o.items[o.sel].act()
	case BtnB, BtnX:
		u.modal = modalNone
	}
}

// ---------- pickers ----------

func (u *UI) openPick(mode OrderMode) {
	s := u.ord.show
	p := &u.pick
	*p = pickState{mode: mode, show: s, series: u.ord.series, chosenS: map[int]bool{}, chosenE: map[EpKey]bool{},
		eps: map[int][]Episode{}, epErr: map[int]string{}, loadingS: map[int]bool{}}
	p.seasons = s.RealSeasons()
	u.modal = modalNone
	if p.series != nil {
		for _, ss := range p.series.Seasons {
			if ss.Number > 0 && ss.Monitored {
				p.chosenS[ss.Number] = true
			}
		}
		p.loading++
		id := p.series.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			eps, err := u.env.SonarrEpisodes(ctx, id)
			u.post <- func() { u.gotSonarrEpisodes(id, eps, err) }
		}()
	}
	u.scr = scrPick
}

func (u *UI) gotSonarrEpisodes(id int, eps []SonarrEpisode, err error) {
	p := &u.pick
	if p.series == nil || p.series.ID != id {
		return
	}
	p.loading--
	if err != nil {
		p.err = err.Error()
		return
	}
	p.before = eps
	bySeason := map[int][]Episode{}
	count := map[int]int{}
	for _, e := range eps {
		if e.Season == 0 {
			continue
		}
		bySeason[e.Season] = append(bySeason[e.Season], Episode{Season: e.Season, Number: e.Number, Name: e.Title,
			AirDate: e.AirDate, SonarrID: e.ID, Monitored: e.Monitored, HasFile: e.HasFile})
		count[e.Season]++
		if e.Monitored {
			p.chosenE[EpKey{e.Season, e.Number}] = true
		}
	}
	p.eps = bySeason
	// Sonarr's season list is the one that will be used, so show it
	var seasons []SeasonInfo
	for n, c := range count {
		air := ""
		if len(bySeason[n]) > 0 {
			air = bySeason[n][0].AirDate
		}
		seasons = append(seasons, SeasonInfo{Number: n, Episodes: c, AirDate: air})
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i].Number < seasons[j].Number })
	if len(seasons) > 0 {
		p.seasons = seasons
	}
	p.tab = min(p.tab, max(0, len(p.seasons)-1))
}

// tickPick loads the episodes of the season tab on screen (new shows, from TMDB).
func (u *UI) tickPick() {
	p := &u.pick
	if u.scr != scrPick || p.mode != orderEpisodes || p.series != nil || len(p.seasons) == 0 {
		return
	}
	n := p.seasons[p.tab].Number
	if _, ok := p.eps[n]; ok || p.loadingS[n] || p.epErr[n] != "" {
		return
	}
	p.loadingS[n] = true
	p.loading++
	id := p.show.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		eps, err := u.env.Season(ctx, id, n)
		u.post <- func() {
			if p.show == nil || p.show.ID != id {
				return
			}
			p.loading--
			delete(p.loadingS, n)
			if err != nil {
				p.epErr[n] = netHint(err).Error()
				return
			}
			p.eps[n] = eps
		}
	}()
}

// pickRows is what the list shows: seasons, or the current tab's episodes.
func (u *UI) pickRows() int {
	p := &u.pick
	if p.mode == orderSeasons {
		return len(p.seasons)
	}
	if len(p.seasons) == 0 {
		return 0
	}
	return len(p.eps[p.seasons[p.tab].Number])
}

func (u *UI) curEpisodes() []Episode {
	p := &u.pick
	if len(p.seasons) == 0 {
		return nil
	}
	return p.eps[p.seasons[p.tab].Number]
}

func (u *UI) keyPick(k int) {
	p := &u.pick
	n := u.pickRows()
	switchTab := func(d int) {
		if len(p.seasons) == 0 {
			return
		}
		p.tab = (p.tab + d + len(p.seasons)) % len(p.seasons)
		p.sel, p.scroll = 0, 0
	}
	switch k {
	case BtnUp:
		if n > 0 {
			p.sel = (p.sel - 1 + n) % n
		}
	case BtnDown:
		if n > 0 {
			p.sel = (p.sel + 1) % n
		}
	case BtnLeft, BtnL1:
		if p.mode == orderEpisodes {
			switchTab(-1)
		} else {
			p.sel = max(0, p.sel-u.pickPage())
		}
	case BtnRight, BtnR1:
		if p.mode == orderEpisodes {
			switchTab(1)
		} else {
			p.sel = min(max(0, n-1), p.sel+u.pickPage())
		}
	case BtnA:
		if p.sel >= n {
			return
		}
		if p.mode == orderSeasons {
			sn := p.seasons[p.sel].Number
			p.chosenS[sn] = !p.chosenS[sn]
		} else {
			e := u.curEpisodes()[p.sel]
			key := EpKey{e.Season, e.Number}
			p.chosenE[key] = !p.chosenE[key]
			if p.sel < n-1 {
				p.sel++ // ready for the next one
			}
		}
	case BtnX:
		if p.mode == orderSeasons {
			all := true
			for _, s := range p.seasons {
				all = all && p.chosenS[s.Number]
			}
			for _, s := range p.seasons {
				p.chosenS[s.Number] = !all
			}
		} else {
			eps := u.curEpisodes()
			all := len(eps) > 0
			for _, e := range eps {
				all = all && p.chosenE[EpKey{e.Season, e.Number}]
			}
			for _, e := range eps {
				p.chosenE[EpKey{e.Season, e.Number}] = !all
			}
		}
	case BtnStart:
		u.confirmPick()
	case BtnB:
		u.scr = scrDetail
		if u.cur != p.show {
			u.openDetail(p.show)
		}
	}
}

func (u *UI) pickCount() int {
	p := &u.pick
	n := 0
	if p.mode == orderSeasons {
		for _, s := range p.seasons {
			if p.chosenS[s.Number] {
				n++
			}
		}
		return n
	}
	for _, v := range p.chosenE {
		if v {
			n++
		}
	}
	return n
}

func (u *UI) confirmPick() {
	p := &u.pick
	o := Order{Mode: p.mode}
	if p.mode == orderSeasons {
		for _, s := range p.seasons {
			if p.chosenS[s.Number] {
				o.Seasons = append(o.Seasons, s.Number)
			}
		}
	} else {
		for k, v := range p.chosenE {
			if v {
				o.Episodes = append(o.Episodes, k)
			}
		}
		sort.Slice(o.Episodes, func(i, j int) bool {
			if o.Episodes[i].S != o.Episodes[j].S {
				return o.Episodes[i].S < o.Episodes[j].S
			}
			return o.Episodes[i].E < o.Episodes[j].E
		})
	}
	if len(o.Seasons) == 0 && len(o.Episodes) == 0 && p.series == nil {
		u.showToast("CHOOSE AT LEAST ONE — A TICKS, X TICKS ALL")
		return
	}
	verb := "ORDER "
	if p.series != nil {
		verb = "UPDATE "
	}
	u.runOrder(verb+strings.ToUpper(p.show.Name), o)
}

// ---------- running an order ----------

func (u *UI) runOrder(title string, o Order) {
	s := u.ord.show
	series := u.ord.series
	var before []SonarrEpisode
	if u.scr == scrPick && u.pick.series != nil {
		before = u.pick.before
	}
	u.prog = progressState{title: title, started: time.Now(), steps: []string{"SENDING ORDER"}}
	u.modal = modalProgress
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		step := func(msg string) {
			u.post <- func() { u.prog.steps = append(u.prog.steps, msg) }
		}
		var res *OrderResult
		var err error
		if series != nil {
			res, err = u.env.ChangeShow(ctx, s, series.ID, o, before, step)
		} else {
			res, err = u.env.AddShow(ctx, s, o, step)
		}
		u.post <- func() {
			u.prog.done = true
			u.prog.res = res
			if err != nil {
				u.prog.err = err.Error()
			}
			u.syncSonarr()
		}
	}()
}

func (u *UI) tickProgress() {}
