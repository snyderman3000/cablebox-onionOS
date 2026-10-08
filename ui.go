package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"strings"
	"time"
)

type screen int

const (
	scrSetup screen = iota
	scrGuide
	scrDetail
	scrLineup
	scrPick
	scrBye
)

type modalKind int

const (
	modalNone modalKind = iota
	modalMsg
	modalOrder
	modalProgress
	modalTrailer
)

const (
	detailTTL = 72 * time.Hour
	scoresTTL = 7 * 24 * time.Hour
	perChan   = 40
)

type UI struct {
	env  *Env
	W, H int
	s    float64

	fLogo, fBig, fBody, fSmall, fTiny, fLCD, fOSD *Font

	scr    screen
	guide  *Guide
	lineup []*Channel
	chIdx  int
	tuneAt int // frame the channel changed, for the static
	shows  []*Show
	sel    int
	scroll int
	moved  time.Time

	chLoading map[string]bool
	chErr     map[string]string
	metaBusy  bool // services and genres

	detBusy  map[int]bool
	detErr   map[int]bool
	omdbBusy map[int]bool
	omdbDead string // OMDb gave up (limit, bad key) for this session
	saveAt   time.Time
	dirty    bool

	imgs     map[string]*image.RGBA
	imgOrder []string
	imgBusy  map[string]bool
	imgErr   map[string]bool
	imgSlots chan struct{}

	cur        *Show // details screen
	detailScrl int
	lineupSel  int

	lib       *SonarrLibrary
	libErr    string
	libBusy   bool
	target    *SonarrTarget
	targetErr string

	ord  orderState
	pick pickState
	prog progressState

	modal    modalKind
	msgTitle string
	msgBody  []string
	msgCol   color.RGBA

	toast      string
	toastUntil time.Time

	frame    int
	byeFrame int
	quit     bool
	post     chan func()
}

func NewUI(env *Env, w, h int) *UI {
	s := float64(h) / 480
	if sw := float64(w) / 640; sw < s {
		s = sw
	}
	u := &UI{env: env, W: w, H: h, s: s, post: make(chan func(), 256),
		guide: newGuide(), chLoading: map[string]bool{}, chErr: map[string]string{},
		detBusy: map[int]bool{}, detErr: map[int]bool{}, omdbBusy: map[int]bool{},
		imgs: map[string]*image.RGBA{}, imgBusy: map[string]bool{}, imgErr: map[string]bool{}, imgSlots: make(chan struct{}, 2)}
	u.fLogo = loadFont(titleTTF, 21*s)
	u.fBig = loadFont(titleTTF, 17*s)
	u.fBody = loadFont(bodyTTF, 17*s)
	u.fSmall = loadFont(bodyTTF, 14*s)
	u.fTiny = loadFont(bodyTTF, 12*s)
	u.fLCD = loadFont(lcdTTF, 23*s)
	u.fOSD = loadFont(lcdTTF, 34*s)
	u.lineup = Lineup(nil, nil, nil)
	return u
}

func (u *UI) px(v int) int { return int(math.Round(float64(v) * u.s)) }

// ---------- lifecycle ----------

func (u *UI) Start() {
	u.guide = u.env.LoadGuide()
	u.env.PruneImageCache(60 << 20)
	if p := u.env.Config.Problem; p != "" {
		u.scr = scrSetup
		u.message("CONFIG ERROR", colRed, p)
		return
	}
	if u.env.Config.TMDBKey == "" {
		u.scr = scrSetup
		return
	}
	u.scr = scrGuide
	u.rebuildLineup()
	idx := 0
	for i, ch := range u.lineup {
		if ch.Num == u.guide.Channel {
			idx = i
		}
	}
	u.tune(idx)
	u.refreshMeta(false)
	u.syncSonarr()
	if u.env.Config.OMDbKey == "" {
		u.showToast("ADD AN OMDB KEY FOR IMDB RATINGS")
	}
}

// Stop saves the guide on the way out.
func (u *UI) Stop() {
	if ch := u.channel(); ch != nil {
		u.guide.Channel = ch.Num
	}
	u.env.SaveGuide(u.guide)
}

func (u *UI) channel() *Channel {
	if u.chIdx < 0 || u.chIdx >= len(u.lineup) {
		return nil
	}
	return u.lineup[u.chIdx]
}

func (u *UI) rebuildLineup() {
	keep := ""
	if ch := u.channel(); ch != nil {
		keep = ch.Key
	}
	u.lineup = Lineup(u.env.Config.Services, u.guide.Services, u.guide.Genres)
	for i, ch := range u.lineup {
		if ch.Key == keep {
			u.chIdx = i
		}
	}
	u.chIdx = min(u.chIdx, len(u.lineup)-1)
}

// refreshMeta updates the service and genre lists (weekly, or when missing).
func (u *UI) refreshMeta(force bool) {
	if u.metaBusy || (!force && len(u.guide.Services) > 0 && time.Since(u.guide.ServicesAt) < 7*24*time.Hour) {
		return
	}
	u.metaBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		svcs, err := u.env.Services(ctx)
		genres, gerr := u.env.Genres(ctx)
		u.post <- func() {
			u.metaBusy = false
			if err != nil {
				log.Printf("services: %v", err)
				u.chErr["services"] = err.Error()
				return
			}
			log.Printf("services: %d listed for %s", len(svcs), u.env.Config.Region)
			u.guide.Services = svcs
			u.guide.ServicesAt = u.env.Now()
			if gerr == nil && len(genres) > 0 {
				u.guide.Genres = genres
			}
			before := u.channel()
			u.rebuildLineup()
			u.dirty = true
			// a service channel tuned before the list arrived can load now
			if ch := u.channel(); ch != nil && before != nil && before.Providers == "" && ch.Providers != "" && u.scr == scrGuide {
				u.fetchChannel(ch, false)
			}
		}
	}()
}

// tune switches to channel i, loading its listing if it's missing or stale.
func (u *UI) tune(i int) {
	if len(u.lineup) == 0 {
		return
	}
	i = (i%len(u.lineup) + len(u.lineup)) % len(u.lineup)
	if i != u.chIdx || u.shows == nil {
		u.tuneAt = u.frame
	}
	u.chIdx = i
	u.sel, u.scroll = 0, 0
	u.reloadShows(0)
	ch := u.channel()
	l := u.guide.Lists[ch.Key]
	if l == nil || len(l.IDs) == 0 || time.Since(l.At) > time.Duration(u.env.Config.RefreshHours)*time.Hour {
		u.fetchChannel(ch, false)
	}
	if ch.Kind == chService && ch.Providers == "" && len(u.guide.Services) == 0 {
		u.refreshMeta(true) // the service list never arrived; ask again
	}
	u.moved = time.Now()
}

func (u *UI) reloadShows(keep int) {
	ch := u.channel()
	if ch == nil {
		return
	}
	u.shows = u.guide.ChannelShows(ch.Key)
	sortShows(u.shows, u.guide.Sort)
	u.sel = min(u.sel, max(0, len(u.shows)-1))
	for i, s := range u.shows {
		if s.ID == keep {
			u.sel = i
		}
	}
}

func (u *UI) fetchChannel(ch *Channel, manual bool) {
	if u.chLoading[ch.Key] {
		return
	}
	if ch.Kind == chService && ch.Providers == "" {
		return
	}
	u.chLoading[ch.Key] = true
	delete(u.chErr, ch.Key)
	c := *ch
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		shows, err := u.env.Discover(ctx, &c, perChan)
		u.post <- func() {
			delete(u.chLoading, c.Key)
			if err != nil {
				log.Printf("CH %02d %s failed: %v", c.Num, c.Name, err)
			} else {
				log.Printf("CH %02d %s: %d shows", c.Num, c.Name, len(shows))
			}
			if err != nil {
				msg := netHint(err).Error()
				u.chErr[c.Key] = msg
				if manual || len(u.guide.ChannelShows(c.Key)) > 0 {
					u.showToast(strings.ToUpper(shortErr(msg)))
				}
				return
			}
			u.guide.Merge(c.Key, shows, u.env.Now())
			u.dirty = true
			if cur := u.channel(); cur != nil && cur.Key == c.Key {
				keep := 0
				if s := u.selected(); s != nil {
					keep = s.ID
				}
				u.reloadShows(keep)
				if manual {
					u.showToast(fmt.Sprintf("CH %02d UPDATED · %d SHOWS", c.Num, len(u.shows)))
				}
			}
		}
	}()
}

func shortErr(s string) string {
	if i := strings.Index(s, " — "); i > 0 {
		return s[:i]
	}
	return s
}

func (u *UI) selected() *Show {
	if u.sel < 0 || u.sel >= len(u.shows) {
		return nil
	}
	return u.shows[u.sel]
}

func (u *UI) showToast(s string) {
	u.toast = s
	u.toastUntil = time.Now().Add(3500 * time.Millisecond)
}

func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (u *UI) message(title string, col color.RGBA, body string) {
	u.modal = modalMsg
	u.msgTitle = title
	u.msgCol = col
	u.msgBody = strings.Split(sentence(body), "\n")
}

// ---------- background work ----------

func (u *UI) Tick() {
	u.dispatchDetails()
	if u.scr == scrGuide && time.Since(u.moved) > 200*time.Millisecond {
		if s := u.selected(); s != nil {
			u.poster(s, false, true)
		}
		for d := 1; d <= 2; d++ {
			if i := u.sel + d; i < len(u.shows) {
				u.poster(u.shows[i], false, true)
			}
		}
	}
	if u.dirty && time.Since(u.saveAt) > 15*time.Second {
		u.saveAt = time.Now()
		u.dirty = false
		b, err := json.Marshal(u.guide)
		if err == nil {
			path := u.env.guidePath()
			go func() {
				if os.WriteFile(path+".tmp", b, 0o644) == nil {
					os.Rename(path+".tmp", path)
				}
			}()
		}
	}
	u.tickPick()
	u.tickProgress()
}

func (u *UI) needsDetails(s *Show) bool {
	return !u.detBusy[s.ID] && !u.detErr[s.ID] && (s.DetailAt.IsZero() || u.env.Now().Sub(s.DetailAt) > detailTTL)
}

func (u *UI) needsScores(s *Show) bool {
	return u.env.Config.OMDbKey != "" && u.omdbDead == "" && s.IMDbID != "" && !u.omdbBusy[s.ID] &&
		(s.ScoresAt.IsZero() || u.env.Now().Sub(s.ScoresAt) > scoresTTL)
}

// dispatchDetails keeps up to three detail lookups and two score lookups
// running, nearest the cursor first.
func (u *UI) dispatchDetails() {
	busy, sb := 0, 0
	for _, b := range u.detBusy {
		if b {
			busy++
		}
	}
	for _, b := range u.omdbBusy {
		if b {
			sb++
		}
	}
	var order []*Show
	if u.cur != nil && (u.scr == scrDetail || u.scr == scrPick || u.modal == modalOrder) {
		order = append(order, u.cur)
	}
	for d := 0; d < len(u.shows); d++ {
		if i := u.sel + d; i < len(u.shows) {
			order = append(order, u.shows[i])
		}
		if i := u.sel - d; d > 0 && i >= 0 && d < 4 {
			order = append(order, u.shows[i])
		}
	}
	for _, s := range order {
		if busy >= 3 && sb >= 2 {
			return
		}
		if busy < 3 && u.needsDetails(s) {
			busy++
			u.fetchDetails(s)
		}
		if sb < 2 && !s.DetailAt.IsZero() && u.needsScores(s) {
			sb++
			u.fetchScores(s)
		}
	}
}

// copyShow gives a goroutine its own Show to fill (slices included).
func copyShow(s *Show) *Show {
	cp := *s
	cp.GenreIDs, cp.Genres, cp.Networks, cp.Creators = nil, nil, nil, nil
	cp.Cast, cp.Seasons, cp.Providers = nil, nil, nil
	cp.GenreIDs = append([]int(nil), s.GenreIDs...)
	return &cp
}

func (u *UI) fetchDetails(s *Show) {
	u.detBusy[s.ID] = true
	cp := copyShow(s)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		err := u.env.Details(ctx, cp)
		u.post <- func() {
			delete(u.detBusy, s.ID)
			if err != nil {
				log.Printf("details %d: %v", s.ID, err)
				u.detErr[s.ID] = true
				return
			}
			rt, meta, imdb, votes, awards, scoresAt := s.RT, s.Meta, s.IMDb, s.IMDbVotes, s.Awards, s.ScoresAt
			*s = *cp
			s.RT, s.Meta, s.IMDb, s.IMDbVotes, s.Awards, s.ScoresAt = rt, meta, imdb, votes, awards, scoresAt
			u.dirty = true
		}
	}()
}

func (u *UI) fetchScores(s *Show) {
	u.omdbBusy[s.ID] = true
	cp := &Show{IMDbID: s.IMDbID, Cert: s.Cert, RT: -1, Meta: -1}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := u.env.Scores(ctx, cp)
		u.post <- func() {
			delete(u.omdbBusy, s.ID)
			if err != nil {
				if ae, ok := err.(*APIError); ok && (ae.Status == 429 || ae.Status == 401) {
					u.omdbDead = ae.Msg
					u.showToast(strings.ToUpper(shortErr(ae.Msg)))
				}
				return
			}
			s.RT, s.Meta, s.IMDb, s.IMDbVotes, s.Awards, s.ScoresAt = cp.RT, cp.Meta, cp.IMDb, cp.IMDbVotes, cp.Awards, cp.ScoresAt
			if s.Cert == "" {
				s.Cert = cp.Cert
			}
			u.dirty = true
		}
	}()
}

// syncSonarr reloads which shows are already in Sonarr.
func (u *UI) syncSonarr() {
	if !u.env.SonarrOn() || u.libBusy {
		return
	}
	u.libBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		lib, err := u.env.SonarrLibrary(ctx)
		u.post <- func() {
			u.libBusy = false
			if err != nil {
				u.libErr = err.Error()
				u.showToast("SONARR: " + strings.ToUpper(shortErr(u.libErr)))
				return
			}
			u.lib, u.libErr = lib, ""
		}
	}()
}

func (u *UI) inSonarr(s *Show) *SonarrSeries { return u.lib.Find(s) }

// ---------- images ----------

func (u *UI) posterBox(big bool) (int, int) {
	if big {
		return u.px(176), u.px(264)
	}
	return u.px(112), u.px(168)
}

// poster returns a cached poster, or starts fetching it when fetch is true.
func (u *UI) poster(s *Show, big, fetch bool) (*image.RGBA, bool) {
	if s.Poster == "" {
		return nil, true
	}
	w, h := u.posterBox(big)
	size := "w185"
	if big {
		size = "w342"
	}
	url := u.env.ImageURL(size, s.Poster)
	k := fmt.Sprintf("%s|%dx%d", url, w, h)
	if img := u.imgs[k]; img != nil {
		return img, false
	}
	if u.imgErr[k] {
		return nil, true
	}
	if !fetch || u.imgBusy[k] {
		return nil, false
	}
	u.imgBusy[k] = true
	go func() {
		u.imgSlots <- struct{}{}
		img, err := u.env.Image(url, w, h, true)
		<-u.imgSlots
		u.post <- func() {
			delete(u.imgBusy, k)
			if err != nil {
				u.imgErr[k] = true
				return
			}
			u.imgs[k] = img
			u.imgOrder = append(u.imgOrder, k)
			for len(u.imgOrder) > 40 {
				delete(u.imgs, u.imgOrder[0])
				u.imgOrder = u.imgOrder[1:]
			}
		}
	}()
	return nil, false
}

func (u *UI) Animating() bool {
	toast := u.toast != "" && time.Now().Before(u.toastUntil.Add(100*time.Millisecond))
	switch {
	case u.scr == scrBye, toast, u.frame-u.tuneAt < 8:
		return true
	case u.modal == modalProgress && !u.prog.done:
		return u.frame%2 == 0
	case len(u.imgBusy) > 0 || len(u.detBusy) > 0:
		return u.frame%3 == 0
	case u.chLoading[u.chanKey()]:
		return u.frame%2 == 0
	case u.scr == scrPick && u.pick.loading > 0:
		return u.frame%3 == 0
	}
	return false
}

func (u *UI) chanKey() string {
	if ch := u.channel(); ch != nil {
		return ch.Key
	}
	return ""
}

// ---------- input ----------

func (u *UI) Key(k int) {
	if k == BtnMenu {
		u.quit = true
		return
	}
	switch u.modal {
	case modalMsg, modalTrailer:
		if k == BtnA || k == BtnB || k == BtnStart || (u.modal == modalTrailer && k == BtnY) {
			u.modal = modalNone
		}
		return
	case modalOrder:
		u.keyOrder(k)
		return
	case modalProgress:
		if u.prog.done && (k == BtnA || k == BtnB) {
			u.modal = modalNone
			if u.prog.err == "" && u.scr == scrPick {
				u.scr = scrDetail
			}
		}
		return
	}
	switch u.scr {
	case scrSetup:
		switch k {
		case BtnA, BtnStart:
			u.env.Config = loadConfig(u.env.DataDir+"/config.json", u.env.AppDir+"/../Rewind/data/config.json")
			if p := u.env.Config.Problem; p != "" {
				u.message("CONFIG ERROR", colRed, p)
				return
			}
			if u.env.Config.TMDBKey == "" {
				u.showToast("STILL NO TMDB KEY IN CONFIG.JSON")
				return
			}
			u.Start()
		case BtnB:
			u.quit = true
		}
	case scrGuide:
		u.keyGuide(k)
	case scrDetail:
		u.keyDetail(k)
	case scrLineup:
		u.keyLineup(k)
	case scrPick:
		u.keyPick(k)
	}
}

func (u *UI) keyGuide(k int) {
	n := len(u.shows)
	rows := u.rowsPerPage()
	move := func(to int) {
		if n == 0 {
			return
		}
		u.sel = (to%n + n) % n
		u.moved = time.Now()
	}
	switch k {
	case BtnUp:
		move(u.sel - 1)
	case BtnDown:
		move(u.sel + 1)
	case BtnLeft:
		u.sel = max(0, u.sel-rows)
		u.moved = time.Now()
	case BtnRight:
		u.sel = min(max(0, n-1), u.sel+rows)
		u.moved = time.Now()
	case BtnL1:
		u.tune(u.chIdx - 1)
	case BtnR1:
		u.tune(u.chIdx + 1)
	case BtnL2, BtnR2:
		u.jumpGroup(k == BtnR2)
	case BtnA:
		if s := u.selected(); s != nil {
			u.openDetail(s)
		}
	case BtnX:
		if s := u.selected(); s != nil {
			u.openOrder(s)
		}
	case BtnY:
		keep := 0
		if s := u.selected(); s != nil {
			keep = s.ID
		}
		u.guide.Sort = (u.guide.Sort + 1) % len(sortNames)
		u.reloadShows(keep)
		u.showToast("SORT: " + sortNames[u.guide.Sort])
		u.dirty = true
	case BtnStart:
		u.scr = scrLineup
		u.lineupSel = u.chIdx
	case BtnSelect:
		if ch := u.channel(); ch != nil {
			u.showToast("REFRESHING CH " + fmt.Sprintf("%02d", ch.Num))
			if ch.Kind == chService && ch.Providers == "" {
				u.refreshMeta(true)
			}
			u.fetchChannel(ch, true)
			u.syncSonarr()
		}
	case BtnB:
		u.quit = true
	}
}

// jumpGroup moves to the first channel of the next (or previous) group.
func (u *UI) jumpGroup(fwd bool) {
	cur := u.channel()
	if cur == nil {
		return
	}
	var starts []int
	for i, ch := range u.lineup {
		if i == 0 || u.lineup[i-1].Group != ch.Group {
			starts = append(starts, i)
		}
	}
	at := 0
	for gi, st := range starts {
		if st <= u.chIdx {
			at = gi
		}
	}
	if fwd {
		at = (at + 1) % len(starts)
	} else {
		at = (at - 1 + len(starts)) % len(starts)
	}
	u.tune(starts[at])
}

func (u *UI) openDetail(s *Show) {
	u.cur = s
	u.detailScrl = 0
	u.scr = scrDetail
	u.poster(s, true, true)
}

func (u *UI) keyDetail(k int) {
	s := u.cur
	if s == nil {
		u.scr = scrGuide
		return
	}
	step := func(d int) {
		for i, x := range u.shows {
			if x.ID == s.ID {
				j := (i + d + len(u.shows)) % len(u.shows)
				u.sel = j
				u.openDetail(u.shows[j])
				return
			}
		}
	}
	switch k {
	case BtnUp:
		u.detailScrl = max(0, u.detailScrl-1)
	case BtnDown:
		u.detailScrl++
	case BtnL1:
		step(-1)
	case BtnR1:
		step(1)
	case BtnX:
		u.openOrder(s)
	case BtnY:
		if s.Trailer != "" {
			u.modal = modalTrailer
		} else {
			u.showToast("NO TRAILER LISTED FOR THIS SHOW")
		}
	case BtnB:
		u.scr = scrGuide
	}
}

func (u *UI) keyLineup(k int) {
	n := len(u.lineup)
	rows := u.lineupRows()
	switch k {
	case BtnUp:
		u.lineupSel = (u.lineupSel - 1 + n) % n
	case BtnDown:
		u.lineupSel = (u.lineupSel + 1) % n
	case BtnLeft:
		u.lineupSel = max(0, u.lineupSel-rows)
	case BtnRight:
		u.lineupSel = min(n-1, u.lineupSel+rows)
	case BtnA:
		u.scr = scrGuide
		u.tune(u.lineupSel)
	case BtnB, BtnStart:
		u.scr = scrGuide
	}
}

// ---------- device ----------

func wifiUp() bool {
	b, err := os.ReadFile("/sys/class/net/wlan0/operstate")
	return err == nil && strings.TrimSpace(string(b)) == "up"
}

func battery() int {
	b, err := os.ReadFile("/tmp/percBat")
	if err != nil {
		return -1
	}
	var v int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &v); err != nil || v < 0 || v > 100 {
		return -1
	}
	return v
}
