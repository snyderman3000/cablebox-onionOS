package main

// Drawing. An 80s cable converter box along the top (woodgrain, red LED
// channel number, a VFD window), a Prevue-style navy program guide on the
// screen, and the family's beige keycaps along the bottom.

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"
)

// ---------- layout ----------

func (u *UI) topH() int        { return u.px(62) }
func (u *UI) keysH() int       { return u.px(44) }
func (u *UI) contentBot() int  { return u.H - u.keysH() }
func (u *UI) headH() int       { return u.px(32) }
func (u *UI) listTop() int     { return u.topH() + u.headH() + u.px(6) }
func (u *UI) rowH() int        { return u.px(41) }
func (u *UI) listW() int       { return u.W * 61 / 100 }
func (u *UI) rowsPerPage() int { return max(1, (u.contentBot()-u.listTop()-u.px(4))/u.rowH()) }

// ---------- frame ----------

func (u *UI) Draw(c *Canvas) {
	c.Fill(0, 0, c.W, c.H, colGuide2)
	if u.scr == scrBye {
		u.drawBye(c)
		return
	}
	u.drawBox(c)
	c.VGrad(0, u.topH(), c.W, u.contentBot()-u.topH(), colGuide, colGuide2)
	switch u.scr {
	case scrSetup:
		u.drawSetup(c)
	case scrGuide:
		u.drawGuide(c)
	case scrDetail:
		u.drawDetail(c)
	case scrLineup:
		u.drawLineup(c)
	case scrPick:
		u.drawPick(c)
	case scrKeyboard:
		u.drawKeyboard(c)
	}
	switch u.modal {
	case modalMsg:
		u.drawMsg(c)
		u.keys(c, [][2]string{{"A", "OK"}})
	case modalOrder:
		u.drawOrder(c)
		u.keys(c, [][2]string{{"A", "CHOOSE"}, {"↕", "MOVE"}, {"B", "CANCEL"}})
	case modalProgress:
		u.drawProgress(c)
		if u.prog.done {
			u.keys(c, [][2]string{{"A", "OK"}})
		} else {
			u.keys(c, nil)
		}
	case modalTrailer:
		u.drawTrailer(c)
		u.keys(c, [][2]string{{"B", "CLOSE"}})
	}
	c.Scanlines(0, u.topH(), c.W, u.contentBot()-u.topH(), 0.07)
	if u.toast != "" && time.Now().Before(u.toastUntil) {
		t := u.fSmall.Ellipsize(u.toast, c.W-u.px(60))
		w := u.fSmall.Width(t) + u.px(24)
		h := u.fSmall.height + u.px(10)
		x, y := (c.W-w)/2, u.contentBot()-h-u.px(8)
		c.Fill(x, y, w, h, colYellow)
		c.Text(u.fSmall, x+u.px(12), y+u.px(5), t, colGuide2)
	}
}

// Woodgrain draws the top of the converter's walnut-veneer lid.
func (c *Canvas) Woodgrain(x, y, w, h int) {
	c.VGrad(x, y, w, h, colWood, colWoodDk)
	for i := 0; i < h; i++ {
		for xx := x; xx < x+w; xx += 2 {
			v := math.Sin(float64(xx)*0.013+float64(i)*0.9+math.Sin(float64(xx)*0.05)*2) * 0.5
			if v > 0.35 {
				c.Blend(xx, y+i, 2, 1, colBlack, 0.18)
			} else if v < -0.42 {
				c.Blend(xx, y+i, 2, 1, rgb(0xb0, 0x78, 0x40), 0.12)
			}
		}
	}
}

// drawBox: the cable converter along the top of the screen.
func (u *UI) drawBox(c *Canvas) {
	h := u.topH()
	wh := u.px(9)
	c.Woodgrain(0, 0, c.W, wh)
	c.VGrad(0, wh, c.W, h-wh, colFace, rgb(0x0e, 0x0c, 0x0b))
	c.Fill(0, wh, c.W, 1, rgb(0x6a, 0x66, 0x60))
	c.Fill(0, h-1, c.W, 1, rgb(0x40, 0x3c, 0x38))
	lx := u.px(16)
	c.Text(u.fLogo, lx, u.px(17), "CABLEBOX", rgb(0xe8, 0xe0, 0xcc))
	c.Text(u.fTiny, lx+u.px(1), u.px(42), "CONVERTER · 64 CHANNEL", rgb(0x8a, 0x84, 0x78))

	// VFD window: channel name and status indicators
	vx, vy, vw, vh := u.px(196), u.px(15), u.px(286), u.px(40)
	c.Fill(vx, vy, vw, vh, rgb(0x04, 0x0c, 0x0c))
	c.Border(vx, vy, vw, vh, 1, rgb(0x30, 0x2c, 0x28))
	vfd := rgb(0x5a, 0xf0, 0xd0)
	vdim := mix(rgb(0x04, 0x0c, 0x0c), vfd, 0.18)
	name := "NO SIGNAL"
	switch {
	case u.scr == scrSetup:
		name = "SETUP"
	case u.scr == scrKeyboard:
		name = "SEARCH"
	case u.channel() != nil:
		name = u.channel().Name
	}
	c.Text(u.fLCD, vx+u.px(10), vy+u.px(2), u.fLCD.Ellipsize(name, vw-u.px(90)), vfd)
	clock := strings.TrimPrefix(time.Now().Format("3:04PM"), "0")
	c.TextRight(u.fLCD, vx+vw-u.px(8), vy+u.px(2), clock, vfd)
	ix := vx + u.px(10)
	iy := vy + vh - u.fTiny.height - u.px(3)
	lit := func(s string, on bool, col color.RGBA) {
		if !on {
			col = vdim
		}
		ix = c.Text(u.fTiny, ix, iy, s, col) + u.px(10)
	}
	busy := u.chLoading[u.chanKey()] || len(u.detBusy) > 0
	lit("TUNE", busy && u.frame/4%2 == 0, vfd)
	lit("SONARR", u.lib != nil, vfd)
	lit("ORDER", u.modal == modalProgress && !u.prog.done && u.frame/5%2 == 0, colYellow)
	lit("WIFI", wifiUp(), vfd)
	if b := battery(); b >= 0 {
		col := vfd
		if b <= 15 {
			col = colOrange
		}
		c.TextRight(u.fTiny, vx+vw-u.px(8), iy, fmt.Sprintf("%d%%", b), col)
	}

	// red LED channel number
	rx := u.px(494)
	rw := c.W - u.px(14) - rx
	bg := rgb(0x12, 0x02, 0x02)
	c.Fill(rx, vy, rw, vh, bg)
	c.Border(rx, vy, rw, vh, 1, rgb(0x30, 0x2c, 0x28))
	num := "--"
	if ch := u.channel(); ch != nil && u.scr != scrSetup {
		num = fmt.Sprintf("%02d", ch.Num)
	}
	if u.scr == scrKeyboard {
		num = "01"
	}
	dw, dh, t := u.px(17), u.px(28), max(2, u.px(3))
	sw := Seg7Width(dw, t, num)
	c.Seg7(rx+rw-u.px(10)-sw, vy+u.px(6), dw, dh, t, num, rgb(0xff, 0x26, 0x14), bg)
	c.Text(u.fTiny, rx+u.px(8), vy+u.px(6), "CH", rgb(0x8a, 0x20, 0x14))
	// channel up/down rocker
	c.Fill(rx+u.px(8), vy+u.px(22), u.px(18), u.px(14), rgb(0x2a, 0x26, 0x22))
	c.Text(u.fTiny, rx+u.px(11), vy+u.px(22), "▲▼", rgb(0x8a, 0x84, 0x78))
}

// keys: beige keycaps along the bottom. A caption starting with ! is printed red.
func (u *UI) keys(c *Canvas, items [][2]string) {
	y := u.contentBot()
	h := u.keysH()
	c.Fill(0, y, c.W, h, colKeybed)
	c.Fill(0, y, c.W, 1, rgb(0x55, 0x4e, 0x46))
	pad, gap := u.px(18), u.px(7)
	total := 0
	for _, it := range items {
		total += u.fSmall.Width(it[0]) + u.fTiny.Width(" "+strings.TrimPrefix(it[1], "!")) + pad + gap
	}
	if total > c.W-u.px(20) {
		pad, gap = u.px(10), u.px(4)
	}
	x := u.px(10)
	ky, kh := y+u.px(7), u.px(31)
	for _, it := range items {
		caption := strings.TrimPrefix(it[1], "!")
		w := u.fSmall.Width(it[0]) + u.fTiny.Width(" "+caption) + pad
		c.Keycap(x, ky, w, kh)
		bx := c.Text(u.fSmall, x+pad/2, ky+u.px(6), it[0], colCapTxt)
		col := rgb(0x55, 0x4e, 0x44)
		if caption != it[1] {
			col = rgb(0xb0, 0x2a, 0x1e)
		}
		c.Text(u.fTiny, bx+u.fTiny.Width(" "), ky+u.px(8), caption, col)
		x += w + gap
	}
}

// cell draws a program-guide cell.
func (u *UI) cell(c *Canvas, x, y, w, h int, sel bool) {
	if sel {
		c.Fill(x, y, w, h, colYellow)
		c.Border(x, y, w, h, 1, rgb(0xff, 0xf6, 0xb0))
		return
	}
	c.VGrad(x, y, w, h, colCellHi, colCell)
	c.Border(x, y, w, h, 1, colCellLn)
	c.Fill(x+1, y+h-1, w-2, 1, colGuide2)
}

// badge draws a score tag like [IMDb 8.4] and returns the x after it.
func (u *UI) badge(c *Canvas, x, y int, label, value string, lbg, lfg color.RGBA) int {
	f := u.fSmall
	h := f.height + u.px(4)
	lw := f.Width(label) + u.px(8)
	c.Fill(x, y, lw, h, lbg)
	c.Text(f, x+u.px(4), y+u.px(2), label, lfg)
	vw := f.Width(value) + u.px(10)
	c.Fill(x+lw, y, vw, h, colGuide2)
	c.Border(x+lw, y, vw, h, 1, lbg)
	c.Text(f, x+lw+u.px(5), y+u.px(2), value, colPaper)
	return x + lw + vw + u.px(6)
}

// scoreBadges draws IMDb, Rotten Tomatoes, Metacritic and TMDB tags that exist.
func (u *UI) scoreBadges(c *Canvas, x, y, maxX int, s *Show) int {
	start := x
	try := func(label, value string, bg, fg color.RGBA) {
		w := u.fSmall.Width(label) + u.fSmall.Width(value) + u.px(24)
		if x+w > maxX && x != start {
			return
		}
		x = u.badge(c, x, y, label, value, bg, fg)
	}
	if s.IMDb > 0 {
		v := fmt.Sprintf("%.1f", s.IMDb)
		if s.IMDbVotes != "" {
			v += " (" + shortVotes(s.IMDbVotes) + ")"
		}
		try("IMDb", v, rgb(0xf5, 0xc5, 0x18), colBlack)
	}
	if s.RT >= 0 {
		bg := colTomato
		if s.RT < 60 {
			bg = colSplat
		}
		try("RT", fmt.Sprintf("%d%%", s.RT), bg, colWhite)
	}
	if s.Meta >= 0 {
		bg := colGreenOK
		switch {
		case s.Meta < 40:
			bg = colRed
		case s.Meta < 61:
			bg = colYellow
		}
		try("META", fmt.Sprint(s.Meta), bg, colBlack)
	}
	if s.VoteCount >= 10 {
		try("TMDB", fmt.Sprintf("%d%%", int(math.Round(s.VoteAvg*10))), rgb(0x22, 0xb8, 0xd8), colBlack)
	}
	return x
}

// shortVotes turns "123,456" into "123K".
func shortVotes(v string) string {
	n := 0
	for _, r := range v {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprint(n)
}

// ratingText is the number shown in the guide's rating column.
func ratingText(s *Show) (string, bool) {
	switch {
	case s.IMDb > 0:
		return fmt.Sprintf("%.1f", s.IMDb), true
	case s.VoteCount >= 10:
		return fmt.Sprintf("%.1f", s.VoteAvg), false
	}
	return "--", false
}

// sonarrMark: ● every episode on disk, ◐ some, ○ ordered but nothing yet.
func (u *UI) sonarrMark(s *Show) (string, string) {
	ss := u.inSonarr(s)
	if ss == nil {
		return "", ""
	}
	st := ss.Stats
	switch {
	case st.EpisodeCount > 0 && st.EpisodeFileCount >= st.EpisodeCount:
		return "●", fmt.Sprintf("IN SONARR · ALL %d WANTED EPISODES ON DISK", st.EpisodeCount)
	case st.EpisodeFileCount > 0:
		return "●", fmt.Sprintf("IN SONARR · %d OF %d WANTED EPISODES ON DISK", st.EpisodeFileCount, st.EpisodeCount)
	}
	if st.EpisodeCount == 0 {
		return "○", "IN SONARR · NEW EPISODES ONLY"
	}
	return "○", fmt.Sprintf("IN SONARR · 0 OF %d WANTED EPISODES YET", st.EpisodeCount)
}

// ---------- the guide ----------

func (u *UI) drawGuide(c *Canvas) {
	ch := u.channel()
	top := u.topH()
	// header strip
	c.Fill(0, top, c.W, u.headH(), colGuide2)
	c.Fill(0, top+u.headH()-1, c.W, 1, colCellLn)
	if ch != nil {
		tag := fmt.Sprintf("CH %02d", ch.Num)
		tw := u.fLCD.Width(tag) + u.px(16)
		c.Fill(u.px(10), top+u.px(4), tw, u.headH()-u.px(8), colYellow)
		c.Text(u.fLCD, u.px(18), top+u.px(4), tag, colGuide2)
		x := c.Text(u.fBig, u.px(18)+tw, top+u.px(8), ch.Name, colPaper)
		if ch.Kind == chSearch && u.guide.Search != "" {
			c.Text(u.fSmall, x+u.px(10), top+u.px(9), u.fSmall.Ellipsize("\""+strings.ToUpper(u.guide.Search)+"\"", c.W-x-u.px(150)), colYellow)
		} else {
			c.Text(u.fTiny, x+u.px(10), top+u.px(12), ch.Group, colPaperFt)
		}
	}
	info := fmt.Sprintf("%d SHOWS", len(u.shows))
	if u.guide.Sort != 0 {
		info = "BY " + sortNames[u.guide.Sort] + " · " + info
	}
	c.TextRight(u.fSmall, c.W-u.px(14), top+u.px(9), info, colPaperDm)

	lw := u.listW()
	rows := u.rowsPerPage()
	rh := u.rowH()
	n := len(u.shows)
	if u.sel < u.scroll {
		u.scroll = u.sel
	}
	if u.sel >= u.scroll+rows {
		u.scroll = u.sel - rows + 1
	}
	u.scroll = max(0, min(u.scroll, n-rows))
	for r := 0; r < rows && u.scroll+r < n; r++ {
		i := u.scroll + r
		u.drawRow(c, u.shows[i], u.px(10), u.listTop()+r*rh, lw-u.px(14), rh-u.px(3), i == u.sel)
	}
	if n > rows {
		sbx := lw - u.px(1)
		sy, sh := u.listTop(), rows*rh-u.px(3)
		c.Fill(sbx, sy, u.px(3), sh, colGuide2)
		th := max(u.px(12), sh*rows/n)
		c.Fill(sbx, sy+(sh-th)*u.scroll/max(1, n-rows), u.px(3), th, colCellLn)
	}
	listH := u.contentBot() - u.listTop() - u.px(4)
	loading := ch != nil && u.chLoading[ch.Key]
	switch {
	case n == 0 && loading:
		c.Noise(u.px(10), u.listTop(), lw-u.px(14), listH, uint32(u.frame))
		msg := fmt.Sprintf("TUNING CH %02d…", ch.Num)
		w := u.fBig.Width(msg) + u.px(24)
		bh := u.px(32)
		slow := time.Since(u.chSince[ch.Key]) > 12*time.Second
		if slow {
			w = max(w, u.fTiny.Width("TMDB IS SLOW TO ANSWER — STILL TRYING")+u.px(24))
			bh += u.fTiny.height + u.px(4)
		}
		c.Fill(u.px(10)+(lw-u.px(14)-w)/2, u.listTop()+listH/2-u.px(16), w, bh, colGuide2)
		c.TextCenter(u.fBig, u.px(10)+(lw-u.px(14))/2, u.listTop()+listH/2-u.px(9), msg, colPaper)
		if slow {
			c.TextCenter(u.fTiny, u.px(10)+(lw-u.px(14))/2, u.listTop()+listH/2+u.px(14), "TMDB IS SLOW TO ANSWER — STILL TRYING", colYellow)
		}
	case n == 0 && ch != nil:
		if ch.Kind != chSearch {
			c.Noise(u.px(10), u.listTop(), lw-u.px(14), listH, uint32(u.frame/20))
		}
		bx, bw := u.px(22), lw-u.px(38)
		lines := []string{"Nothing on this channel."}
		title := "NO SIGNAL"
		if ch.Kind == chSearch {
			title = "SEARCH"
			msg := "Press SELECT (or A) and type a show's name."
			if u.guide.Search != "" {
				msg = fmt.Sprintf("No shows called \"%s\". Press SELECT to try another name.", u.guide.Search)
			}
			lines = u.fSmall.Wrap(msg, bw-u.px(20))
		}
		if e := u.chErr[ch.Key]; e != "" {
			lines = u.fSmall.Wrap(sentence(e), bw-u.px(20))
		} else if ch.Kind == chService && ch.Providers == "" {
			why := "Waiting for TMDB's list of streaming services."
			if e := u.chErr["services"]; e != "" {
				why = "TMDB's list of streaming services didn't load: " + e
			}
			lines = u.fSmall.Wrap(why, bw-u.px(20))
		}
		if ch.Kind != chSearch {
			lines = append(lines, "", "START, then X on this channel, tries again.")
		}
		bh := u.px(46) + len(lines)*(u.fSmall.height+u.px(2))
		by := u.listTop() + (listH-bh)/2
		c.Fill(bx, by, bw, bh, colGuide2)
		c.Border(bx, by, bw, bh, 1, colCellLn)
		c.Text(u.fBig, bx+u.px(10), by+u.px(8), title, colYellow)
		for i, l := range lines {
			c.Text(u.fSmall, bx+u.px(10), by+u.px(34)+i*(u.fSmall.height+u.px(2)), l, colPaper)
		}
	}
	// the channel-change static, and the OSD number
	if d := u.frame - u.tuneAt; d < 6 && ch != nil {
		c.Noise(0, u.topH(), c.W, u.contentBot()-u.topH(), uint32(u.frame*7+3))
	}
	if d := u.frame - u.tuneAt; d < 40 && ch != nil {
		t := fmt.Sprintf("%02d", ch.Num)
		c.Text(u.fOSD, u.px(24)+2, u.listTop()+u.px(6)+2, t, colBlack)
		c.Text(u.fOSD, u.px(24), u.listTop()+u.px(6), t, rgb(0x5a, 0xff, 0x6a))
	}

	if s := u.selected(); s != nil {
		u.drawSide(c, s, lw+u.px(8))
	}
	items := [][2]string{{"A", "INFO"}}
	if u.env.SonarrOn() {
		items = append(items, [2]string{"X", "ORDER"})
	}
	u.keys(c, append(items, [2]string{"L1·R1", "CHANNEL"}, [2]string{"START", "GUIDE"}, [2]string{"SELECT", "SEARCH"}, [2]string{"Y", "SORT"}, [2]string{"B", "OFF"}))
}

func (u *UI) drawRow(c *Canvas, s *Show, x, y, w, h int, sel bool) {
	u.cell(c, x, y, w, h, sel)
	fg, dim, rc := colPaper, colPaperDm, colYellow
	if sel {
		fg, dim, rc = colGuide2, rgb(0x3a, 0x40, 0x7a), colGuide2
	}
	rt, imdb := ratingText(s)
	rw := u.fLCD.Width("8.8") + u.px(8)
	mark, _ := u.sonarrMark(s)
	nameW := w - rw - u.px(20)
	if mark != "" {
		nameW -= u.px(16)
	}
	c.Text(u.fBody, x+u.px(8), y+u.px(3), u.fBody.Ellipsize(s.Name, nameW), fg)
	meta := s.MetaLine()
	if meta == "" && s.FirstAir != "" {
		meta = s.FirstAir[:min(4, len(s.FirstAir))]
	}
	c.Text(u.fTiny, x+u.px(8), y+u.px(22), u.fTiny.Ellipsize(meta, nameW), dim)
	if !imdb && rt != "--" {
		rc = dim
	}
	c.TextRight(u.fLCD, x+w-u.px(8), y+u.px(7), rt, rc)
	if mark != "" {
		mc := colGreenOK
		if sel {
			mc = rgb(0x10, 0x6a, 0x20)
		}
		c.Text(u.fSmall, x+w-rw-u.px(18), y+u.px(11), mark, mc)
	}
}

// drawSide: the selected show's poster, ratings and where to watch it.
func (u *UI) drawSide(c *Canvas, s *Show, x int) {
	w := c.W - x - u.px(10)
	y := u.listTop()
	pw, ph := u.posterBox(false)
	px := x + (w-pw)/2
	c.Fill(px-u.px(3), y-u.px(3), pw+u.px(6), ph+u.px(6), colPaper)
	if img, failed := u.poster(s, false, false); img != nil {
		c.DrawImage(img, px, y, pw, ph)
	} else {
		c.Fill(px, y, pw, ph, colCell)
		if failed {
			for i, l := range u.fSmall.Wrap(strings.ToUpper(s.Name), pw-u.px(12)) {
				if i == 5 {
					break
				}
				c.TextCenter(u.fSmall, px+pw/2, y+u.px(20)+i*(u.fSmall.height+u.px(1)), l, colPaper)
			}
		} else {
			c.Noise(px, y, pw, ph, uint32(u.frame/2))
		}
	}
	y += ph + u.px(10)
	// rating in red LED digits
	rt, imdb := ratingText(s)
	label := "TMDB"
	if imdb {
		label = "IMDb"
	}
	c.Text(u.fTiny, x+u.px(4), y+u.px(2), label, colPaperDm)
	c.Seg7(x+u.px(4), y+u.px(16), u.px(12), u.px(22), max(2, u.px(3)), rt, colYellow, colGuide)
	rx := x + u.px(70)
	if s.RT >= 0 {
		c.Text(u.fTiny, rx, y+u.px(2), "TOMATOMETER", colPaperDm)
		col := colTomato
		if s.RT < 60 {
			col = colSplat
		}
		c.Circle(rx+u.px(8), y+u.px(27), u.px(8), col, true, 0)
		c.Text(u.fBig, rx+u.px(22), y+u.px(18), fmt.Sprintf("%d%%", s.RT), colPaper)
	} else if s.StatusWord() != "" {
		c.Text(u.fTiny, rx, y+u.px(2), "STATUS", colPaperDm)
		col := colPaper
		if s.Ended() {
			col = colPaperDm
		}
		c.Text(u.fSmall, rx, y+u.px(19), s.StatusWord(), col)
		if s.NextEp != nil && s.NextEp.AirDate != "" {
			c.Text(u.fTiny, rx, y+u.px(36), "NEXT "+shortDate(s.NextEp.AirDate), colYellow)
		}
	}
	y += u.px(48)
	if s.RT >= 0 && s.StatusWord() != "" {
		st := s.StatusWord()
		if s.NextEp != nil && s.NextEp.AirDate != "" {
			st += " · NEXT " + shortDate(s.NextEp.AirDate)
		}
		c.Text(u.fTiny, x+u.px(4), y, u.fTiny.Ellipsize(st, w-u.px(8)), colYellow)
		y += u.fTiny.height + u.px(3)
	}
	if len(s.Genres) > 0 {
		c.Text(u.fTiny, x+u.px(4), y, u.fTiny.Ellipsize(strings.ToUpper(strings.Join(s.Genres, " · ")), w-u.px(8)), colPaperDm)
		y += u.fTiny.height + u.px(3)
	}
	if len(s.Providers) > 0 {
		lines := u.fTiny.Wrap("ON "+strings.ToUpper(strings.Join(s.Providers, " · ")), w-u.px(8))
		for i, l := range lines {
			if i == 2 {
				break
			}
			c.Text(u.fTiny, x+u.px(4), y, l, colPaper)
			y += u.fTiny.height + u.px(1)
		}
	}
	if mark, line := u.sonarrMark(s); mark != "" {
		c.Text(u.fTiny, x+u.px(4), u.contentBot()-u.px(20), u.fTiny.Ellipsize(mark+" "+strings.TrimPrefix(line, "IN SONARR · "), w-u.px(8)), colGreenOK)
	}
}

// shortDate turns 2026-10-14 into OCT 14 (and adds the year when it isn't this year).
func shortDate(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	if t.Year() != time.Now().Year() {
		return strings.ToUpper(t.Format("Jan 2 2006"))
	}
	return strings.ToUpper(t.Format("Jan 2"))
}

// ---------- details ----------

func (u *UI) drawDetail(c *Canvas) {
	s := u.cur
	if s == nil {
		return
	}
	top := u.topH()
	pw, ph := u.posterBox(true)
	px, py := u.px(14), top+u.px(12)
	c.Fill(px-u.px(3), py-u.px(3), pw+u.px(6), ph+u.px(6), colPaper)
	if img, failed := u.poster(s, true, true); img != nil {
		c.DrawImage(img, px, py, pw, ph)
	} else {
		c.Fill(px, py, pw, ph, colCell)
		if !failed {
			c.Noise(px, py, pw, ph, uint32(u.frame/2))
		}
	}
	// Sonarr status under the poster
	sy := py + ph + u.px(10)
	if mark, line := u.sonarrMark(s); mark != "" {
		for i, l := range u.fTiny.Wrap(mark+" "+line, pw+u.px(4)) {
			c.Text(u.fTiny, px, sy+i*(u.fTiny.height+u.px(1)), l, colGreenOK)
		}
	} else if u.env.SonarrOn() {
		c.Text(u.fTiny, px, sy, "NOT IN SONARR — X ORDERS IT", colPaperDm)
	}

	x := px + pw + u.px(18)
	w := c.W - x - u.px(14)
	y := top + u.px(8)
	tl := u.fBig.Wrap(strings.ToUpper(s.Name), w)
	for i, l := range tl {
		if i == 2 {
			break
		}
		c.Text(u.fBig, x, y, l, colPaper)
		y += u.fBig.height + u.px(1)
	}
	if s.Tagline != "" {
		c.Text(u.fTiny, x, y+u.px(1), u.fTiny.Ellipsize(s.Tagline, w), colPaperDm)
		y += u.fTiny.height + u.px(2)
	}
	c.Text(u.fSmall, x, y+u.px(3), u.fSmall.Ellipsize(s.MetaLine(), w), colYellow)
	y += u.fSmall.height + u.px(9)
	if s.IMDb > 0 || s.RT >= 0 || s.Meta >= 0 || s.VoteCount >= 10 {
		u.scoreBadges(c, x, y, x+w, s)
		y += u.fSmall.height + u.px(12)
	}
	kw := u.px(78)
	row := func(k, v string, col color.RGBA) {
		if v == "" {
			return
		}
		lines := u.fSmall.Wrap(v, w-kw)
		c.Text(u.fTiny, x, y+u.px(2), k, colPaperFt)
		for i, l := range lines {
			if i == 2 {
				break
			}
			c.Text(u.fSmall, x+kw, y, l, col)
			y += u.fSmall.height + u.px(1)
		}
		y += u.px(3)
	}
	status := s.StatusWord()
	switch {
	case s.NextEp != nil && s.NextEp.AirDate != "":
		status += fmt.Sprintf(" · S%dE%d AIRS %s", s.NextEp.Season, s.NextEp.Number, shortDate(s.NextEp.AirDate))
	case s.LastEp != nil && s.LastEp.AirDate != "":
		status += fmt.Sprintf(" · LAST AIRED %s", shortDate(s.LastEp.AirDate))
	}
	if s.NumEpisodes > 0 {
		status += fmt.Sprintf(" · %d EPISODES", s.NumEpisodes)
	}
	row("STATUS", status, colPaper)
	row("WATCH ON", strings.ToUpper(strings.Join(s.Providers, ", ")), colPaper)
	row("NETWORK", strings.ToUpper(strings.Join(s.Networks, ", ")), colPaper)
	row("GENRE", strings.ToUpper(strings.Join(s.Genres, ", ")), colPaper)
	row("CREATED BY", strings.Join(s.Creators, ", "), colPaper)
	var stars []string
	for i, cm := range s.Cast {
		if i == 4 {
			break
		}
		stars = append(stars, cm.Name)
	}
	row("STARRING", strings.Join(stars, ", "), colPaper)
	if s.Awards != "" {
		row("AWARDS", s.Awards, colYellow)
	}
	if s.DetailAt.IsZero() && u.detBusy[s.ID] {
		c.Text(u.fSmall, x, y, "TUNING IN THE DETAILS"+strings.Repeat(".", u.frame/5%4), colPaperDm)
		y += u.fSmall.height + u.px(4)
	}
	// overview fills the rest, scrollable
	y += u.px(4)
	c.Fill(x, y-u.px(4), w, 1, colCellLn)
	lines := u.fSmall.Wrap(s.Overview, w)
	lh := u.fSmall.height + u.px(2)
	room := max(1, (u.contentBot()-u.px(8)-y)/lh)
	u.detailScrl = min(u.detailScrl, max(0, len(lines)-room))
	for i := 0; i < room && u.detailScrl+i < len(lines); i++ {
		c.Text(u.fSmall, x, y+i*lh, lines[u.detailScrl+i], colPaperDm)
	}
	if len(lines) > room {
		c.TextRight(u.fTiny, x+w, u.contentBot()-u.px(16), fmt.Sprintf("↕ %d/%d", min(len(lines), u.detailScrl+room), len(lines)), colPaperFt)
	}
	var items [][2]string
	if u.env.SonarrOn() {
		if u.inSonarr(s) != nil {
			items = append(items, [2]string{"X", "MANAGE"})
		} else {
			items = append(items, [2]string{"X", "ORDER"})
		}
	}
	if s.Trailer != "" {
		items = append(items, [2]string{"Y", "TRAILER"})
	}
	u.keys(c, append(items, [2]string{"↕", "SCROLL"}, [2]string{"L1·R1", "PREV/NEXT"}, [2]string{"B", "BACK"}))
}

// ---------- channel guide ----------

func (u *UI) lineupRows() int { return max(1, (u.contentBot()-u.topH()-u.px(46))/u.px(30)) }

func (u *UI) drawLineup(c *Canvas) {
	top := u.topH()
	c.Fill(0, top, c.W, u.headH(), colGuide2)
	c.Text(u.fBig, u.px(14), top+u.px(8), "CHANNEL GUIDE", colYellow)
	c.TextRight(u.fSmall, c.W-u.px(14), top+u.px(9), fmt.Sprintf("%d CHANNELS", len(u.lineup)), colPaperDm)
	rows := u.lineupRows()
	cols := (len(u.lineup) + rows - 1) / rows
	vis := min(cols, 3)
	firstCol := max(0, min(u.lineupSel/rows-1, cols-vis))
	cw := (c.W - u.px(28)) / vis
	ch := u.px(27)
	y0 := top + u.headH() + u.px(8)
	for i, chn := range u.lineup {
		col := i/rows - firstCol
		if col < 0 || col >= vis {
			continue
		}
		x := u.px(14) + col*cw
		y := y0 + (i%rows)*u.px(30)
		sel := i == u.lineupSel
		u.cell(c, x, y, cw-u.px(6), ch, sel)
		fg, num := colPaper, colYellow
		if sel {
			fg, num = colGuide2, colGuide2
		}
		nx := c.Text(u.fLCD, x+u.px(6), y+u.px(1), fmt.Sprintf("%02d", chn.Num), num)
		c.Text(u.fSmall, nx+u.px(8), y+u.px(5), u.fSmall.Ellipsize(chn.Name, cw-u.px(70)), fg)
		if i == u.chIdx {
			c.TextRight(u.fSmall, x+cw-u.px(12), y+u.px(5), "◀", fg)
		}
	}
	u.keys(c, [][2]string{{"A", "TUNE"}, {"X", "REFRESH"}, {"SELECT", "SEARCH"}, {"↕◀▶", "MOVE"}, {"B", "CLOSE"}})
}

// ---------- overlays ----------

// box dims the screen and draws a guide window with a yellow title bar.
func (u *UI) box(c *Canvas, w, h int, title string, accent color.RGBA) (int, int, int) {
	c.Blend(0, u.topH(), c.W, u.contentBot()-u.topH(), colBlack, 0.66)
	x, y := (c.W-w)/2, u.topH()+(u.contentBot()-u.topH()-h)/2
	c.VGrad(x, y, w, h, colCell, colGuide2)
	c.Border(x, y, w, h, 2, accent)
	bh := u.fBody.height + u.px(10)
	c.Fill(x+2, y+2, w-4, bh, accent)
	tc := colGuide2
	if accent == colRed {
		tc = colWhite
	}
	c.Text(u.fBody, x+u.px(12), y+u.px(6), u.fBody.Ellipsize(title, w-u.px(24)), tc)
	return x, y + 2 + bh, y + h
}

func (u *UI) paragraphs(paras []string, f *Font, w int) []string {
	var lines []string
	for _, p := range paras {
		if p == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, f.Wrap(p, w)...)
	}
	return lines
}

func (u *UI) drawMsg(c *Canvas) {
	w := c.W * 84 / 100
	lines := u.paragraphs(u.msgBody, u.fBody, w-u.px(40))
	lh := u.fBody.height + u.px(2)
	lines = lines[:min(len(lines), (u.contentBot()-u.topH()-u.px(110))/lh)]
	h := u.px(80) + len(lines)*lh
	accent := colYellow
	if u.msgCol == colRed {
		accent = colRed
	}
	x, y, _ := u.box(c, w, h, u.msgTitle, accent)
	y += u.px(12)
	for _, l := range lines {
		c.Text(u.fBody, x+u.px(20), y, l, colPaper)
		y += lh
	}
	c.TextRight(u.fSmall, x+w-u.px(16), y+u.px(8), "A OK", colPaperDm)
}

func (u *UI) drawTrailer(c *Canvas) {
	s := u.cur
	if s == nil {
		return
	}
	link := "https://www.youtube.com/watch?v=" + s.Trailer
	mod := u.px(4)
	qs := QRSize(link, mod)
	for mod > 2 && qs > u.px(200) {
		mod--
		qs = QRSize(link, mod)
	}
	w, h := c.W*70/100, qs+u.px(100)
	x, y, _ := u.box(c, w, h, "TRAILER: "+strings.ToUpper(s.Name), colYellow)
	c.QR(x+(w-qs)/2, y+u.px(12), mod, link)
	c.TextCenter(u.fSmall, x+w/2, y+qs+u.px(20), "Scan with your phone to watch it on YouTube", colPaper)
}

// ---------- setup and sign-off ----------

func (u *UI) drawSetup(c *Canvas) {
	y := u.topH() + u.px(10)
	c.Text(u.fBig, u.px(16), y, "NO SIGNAL", colYellow)
	c.Text(u.fSmall, u.px(16)+u.fBig.Width("NO SIGNAL")+u.px(14), y+u.px(2), "CABLEBOX NEEDS A FREE TMDB KEY", colPaperDm)
	y += u.fBig.height + u.px(10)
	colW := (c.W - u.px(44)) / 2
	bh := u.px(196)
	block := func(x int, n, title, need, url, short string) {
		c.VGrad(x, y, colW, bh, colCell, colGuide2)
		c.Border(x, y, colW, bh, 1, colCellLn)
		c.Seg7(x+u.px(10), y+u.px(8), u.px(12), u.px(22), max(2, u.px(3)), n, colYellow, colCell)
		c.Text(u.fSmall, x+u.px(38), y+u.px(6), title, colPaper)
		c.Text(u.fTiny, x+u.px(38), y+u.px(6)+u.fSmall.height, need, colPaperDm)
		mod := u.px(4)
		qs := QRSize(url, mod)
		for mod > 2 && qs > u.px(128) {
			mod--
			qs = QRSize(url, mod)
		}
		c.QR(x+(colW-qs)/2, y+u.px(46), mod, url)
		c.TextCenter(u.fTiny, x+colW/2, y+u.px(46)+qs+u.px(5), short, colPaper)
	}
	block(u.px(16), "1", "TMDB API KEY", "REQUIRED · SHOWS, SEASONS, TRAILERS", "https://www.themoviedb.org/settings/api", "themoviedb.org/settings/api")
	block(u.px(28)+colW, "2", "OMDB API KEY", "FOR IMDB RATINGS", "https://www.omdbapi.com/apikey.aspx", "omdbapi.com/apikey.aspx")
	y += bh + u.px(8)
	c.Text(u.fSmall, u.px(16), y, "Put them in App/Cablebox/data/config.json (Rewind's keys work too):", colPaper)
	y += u.fSmall.height + u.px(4)
	c.Fill(u.px(16), y, c.W-u.px(32), u.px(42), colGuide2)
	c.Text(u.fTiny, u.px(26), y+u.px(5), `"tmdb_api_key": "your TMDB key or read access token",`, colYellow)
	c.Text(u.fTiny, u.px(26), y+u.px(5)+u.fTiny.height+u.px(2), `"omdb_api_key": "your OMDb key",`, colYellow)
	u.keys(c, [][2]string{{"A", "RELOAD CONFIG"}, {"B", "OFF"}})
}

// drawBye: the set switching off — the picture collapses to a line, then a dot.
func (u *UI) drawBye(c *Canvas) {
	u.drawBox(c)
	top, bot := u.topH(), u.contentBot()
	c.Fill(0, top, c.W, bot-top, colBlack)
	cy := (top + bot) / 2
	k := float64(u.byeFrame) / 13
	switch {
	case k < 0.5:
		hh := int(float64(bot-top) * (0.5 - k) * 2)
		c.Fill(0, cy-hh/2, c.W, max(2, hh), mix(colGuide, colWhite, k))
	case k < 0.85:
		lw := int(float64(c.W) * (1 - (k-0.5)/0.35))
		c.Fill((c.W-lw)/2, cy-1, lw, 3, colWhite)
	default:
		c.Circle(c.W/2, cy, u.px(3), colWhite, true, 0)
	}
	c.Fill(0, bot, c.W, u.keysH(), colKeybed)
}
