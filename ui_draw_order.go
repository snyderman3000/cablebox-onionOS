package main

// Drawing for ordering: the order menu, the season and episode pickers, and progress.

import (
	"fmt"
	"strings"
	"time"
)

func (u *UI) drawOrder(c *Canvas) {
	o := &u.ord
	if o.show == nil {
		return
	}
	w := c.W * 86 / 100
	ih := u.fBody.height + u.fTiny.height + u.px(12)
	h := u.px(64) + len(o.items)*ih
	if o.series != nil {
		h += u.fTiny.height + u.px(6) - u.fTiny.height // the library line replaces the footer
	}
	title := "ORDER: " + strings.ToUpper(o.show.Name)
	if o.series != nil {
		title = "IN YOUR LIBRARY: " + strings.ToUpper(o.show.Name)
	}
	x, y, bot := u.box(c, w, h, title, colYellow)
	y += u.px(8)
	if o.series != nil {
		_, line := u.sonarrMark(o.show)
		c.Text(u.fTiny, x+u.px(18), y, strings.TrimPrefix(line, "IN SONARR · "), colGreenOK)
		y += u.fTiny.height + u.px(6)
	}
	for i, it := range o.items {
		sel := i == o.sel
		fg, dim := colPaper, colPaperDm
		if sel {
			c.Fill(x+u.px(8), y-u.px(2), w-u.px(16), ih-u.px(2), colYellow)
			fg, dim = colGuide2, rgb(0x3a, 0x40, 0x7a)
		}
		c.Text(u.fBody, x+u.px(18), y, it.label, fg)
		c.Text(u.fTiny, x+u.px(18), y+u.fBody.height+u.px(1), u.fTiny.Ellipsize(it.desc, w-u.px(36)), dim)
		y += ih
	}
	if o.series != nil {
		return
	}
	foot := "QUALITY AND FOLDER: CHECKING SONARR…"
	switch {
	case u.target != nil:
		foot = fmt.Sprintf("QUALITY %s · FOLDER %s", strings.ToUpper(u.target.Profile), u.target.Root)
	case u.targetErr != "":
		foot = "!! " + strings.ToUpper(shortErr(u.targetErr))
	}
	c.Text(u.fTiny, x+u.px(18), bot-u.fTiny.height-u.px(10), u.fTiny.Ellipsize(foot, w-u.px(36)), colPaperFt)
}

func (u *UI) pickPage() int { return max(1, (u.contentBot()-u.pickTop()-u.px(4))/u.px(36)) }

func (u *UI) pickTop() int {
	if u.pick.mode == orderEpisodes {
		return u.topH() + u.headH() + u.px(40)
	}
	return u.topH() + u.headH() + u.px(8)
}

func (u *UI) drawPick(c *Canvas) {
	p := &u.pick
	if p.show == nil {
		return
	}
	top := u.topH()
	c.Fill(0, top, c.W, u.headH(), colGuide2)
	c.Fill(0, top+u.headH()-1, c.W, 1, colCellLn)
	what := "CHOOSE SEASONS"
	if p.mode == orderEpisodes {
		what = "CHOOSE EPISODES"
	}
	x := c.Text(u.fBig, u.px(14), top+u.px(8), what, colYellow)
	c.Text(u.fSmall, x+u.px(12), top+u.px(9), u.fSmall.Ellipsize(strings.ToUpper(p.show.Name), c.W-x-u.px(150)), colPaper)
	n := u.pickCount()
	unit := "SEASON"
	if p.mode == orderEpisodes {
		unit = "EPISODE"
	}
	c.TextRight(u.fSmall, c.W-u.px(14), top+u.px(9), fmt.Sprintf("%d %s%s CHOSEN", n, unit, plural(n)), colPaperDm)

	if p.mode == orderEpisodes && len(p.seasons) > 0 {
		// season tabs
		ty := top + u.headH() + u.px(6)
		tx := u.px(10)
		first := 0
		// keep the current tab on screen
		for {
			w := 0
			for i := first; i <= p.tab; i++ {
				w += u.fSmall.Width(u.tabLabel(i)) + u.px(18)
			}
			if w <= c.W-u.px(20) || first >= p.tab {
				break
			}
			first++
		}
		if first > 0 {
			tx = c.Text(u.fSmall, tx, ty+u.px(6), "◀ ", colPaperDm)
		}
		for i := first; i < len(p.seasons); i++ {
			label := u.tabLabel(i)
			tw := u.fSmall.Width(label) + u.px(14)
			if tx+tw > c.W-u.px(10) {
				c.Text(u.fSmall, c.W-u.px(20), ty+u.px(6), "▶", colPaperDm)
				break
			}
			u.cell(c, tx, ty, tw, u.px(28), i == p.tab)
			fg := colPaper
			if i == p.tab {
				fg = colGuide2
			}
			c.Text(u.fSmall, tx+u.px(7), ty+u.px(6), label, fg)
			tx += tw + u.px(4)
		}
	}

	rows := u.pickPage()
	rh := u.px(36)
	total := u.pickRows()
	if p.sel < p.scroll {
		p.scroll = p.sel
	}
	if p.sel >= p.scroll+rows {
		p.scroll = p.sel - rows + 1
	}
	p.scroll = max(0, min(p.scroll, total-rows))
	y0 := u.pickTop()
	w := c.W - u.px(24)
	box := func(x, y int, on, sel bool) {
		bc := colPaper
		if sel {
			bc = colGuide2
		}
		s := u.px(14)
		c.Border(x, y, s, s, 2, bc)
		if on {
			c.Fill(x+u.px(3), y+u.px(3), s-u.px(6), s-u.px(6), bc)
		}
	}
	today := time.Now().Format("2006-01-02")
	for r := 0; r < rows && p.scroll+r < total; r++ {
		i := p.scroll + r
		y := y0 + r*rh
		sel := i == p.sel
		u.cell(c, u.px(12), y, w, rh-u.px(3), sel)
		fg, dim := colPaper, colPaperDm
		if sel {
			fg, dim = colGuide2, rgb(0x3a, 0x40, 0x7a)
		}
		if p.mode == orderSeasons {
			sn := p.seasons[i]
			box(u.px(22), y+u.px(9), p.chosenS[sn.Number], sel)
			c.Text(u.fBody, u.px(46), y+u.px(7), fmt.Sprintf("SEASON %d", sn.Number), fg)
			info := fmt.Sprintf("%d EPISODE%s", sn.Episodes, plural(sn.Episodes))
			if len(sn.AirDate) >= 4 {
				info += " · " + sn.AirDate[:4]
			}
			if have, all := u.seasonOnDisk(sn.Number); all > 0 && have > 0 {
				info += fmt.Sprintf(" · %d ON DISK", have)
			}
			c.TextRight(u.fSmall, u.px(12)+w-u.px(12), y+u.px(9), info, dim)
			continue
		}
		e := u.curEpisodes()[i]
		box(u.px(22), y+u.px(9), p.chosenE[EpKey{e.Season, e.Number}], sel)
		code := fmt.Sprintf("%dx%02d", e.Season, e.Number)
		cx := c.Text(u.fSmall, u.px(46), y+u.px(9), code, dim)
		right := ""
		rc := dim
		switch {
		case e.HasFile:
			right, rc = "● ON DISK", colGreenOK
			if sel {
				rc = rgb(0x10, 0x6a, 0x20)
			}
		case e.AirDate == "":
			right = "TBA"
		case e.AirDate > today:
			right = "AIRS " + shortDate(e.AirDate)
		default:
			right = shortDate(e.AirDate)
		}
		rw := u.fSmall.Width(right)
		c.TextRight(u.fSmall, u.px(12)+w-u.px(12), y+u.px(9), right, rc)
		c.Text(u.fBody, cx+u.px(10), y+u.px(7), u.fBody.Ellipsize(e.Name, u.px(12)+w-u.px(30)-rw-cx), fg)
	}
	status := ""
	switch {
	case p.err != "":
		status = "!! " + p.err
	case p.loading > 0 && total == 0:
		status = "LOADING EPISODES" + strings.Repeat(".", u.frame/4%4)
	case p.mode == orderEpisodes && len(p.seasons) > 0 && p.epErr[p.seasons[p.tab].Number] != "":
		status = "!! " + p.epErr[p.seasons[p.tab].Number]
	case total == 0:
		status = "NOTHING LISTED YET"
	}
	if status != "" {
		for i, l := range u.fBody.Wrap(status, c.W-u.px(48)) {
			c.Text(u.fBody, u.px(24), y0+u.px(10)+i*(u.fBody.height+u.px(2)), l, colPaper)
		}
	}
	if total > rows {
		sbx := c.W - u.px(9)
		sh := rows * rh
		c.Fill(sbx, y0, u.px(3), sh, colGuide2)
		th := max(u.px(12), sh*rows/total)
		c.Fill(sbx, y0+(sh-th)*p.scroll/max(1, total-rows), u.px(3), th, colCellLn)
	}
	verb := "ORDER"
	if p.series != nil {
		verb = "SAVE"
	}
	items := [][2]string{{"A", "TICK"}}
	if p.mode == orderEpisodes {
		items = append(items, [2]string{"X", "WHOLE SEASON"}, [2]string{"◀▶", "SEASON"})
	} else {
		items = append(items, [2]string{"X", "ALL / NONE"})
	}
	u.keys(c, append(items, [2]string{"START", fmt.Sprintf("%s %d", verb, n)}, [2]string{"B", "BACK"}))
}

// tabLabel: "S2 · 3" when three episodes of season 2 are chosen.
func (u *UI) tabLabel(i int) string {
	p := &u.pick
	sn := p.seasons[i].Number
	n := 0
	for k, v := range p.chosenE {
		if v && k.S == sn {
			n++
		}
	}
	if n > 0 {
		return fmt.Sprintf("S%d · %d", sn, n)
	}
	return fmt.Sprintf("S%d", sn)
}

// seasonOnDisk counts a season's downloaded episodes (shows already in Sonarr).
func (u *UI) seasonOnDisk(n int) (int, int) {
	if u.pick.series == nil {
		return 0, 0
	}
	for _, s := range u.pick.series.Seasons {
		if s.Number == n && s.Stats != nil {
			return s.Stats.EpisodeFileCount, s.Stats.TotalEpisodeCount
		}
	}
	return 0, 0
}

func (u *UI) drawProgress(c *Canvas) {
	pr := &u.prog
	w := c.W * 84 / 100
	var body []string
	if pr.done {
		if pr.err != "" {
			body = u.paragraphs([]string{sentence(pr.err)}, u.fBody, w-u.px(40))
		} else if pr.res != nil {
			body = u.paragraphs(append([]string{pr.res.Summary}, pr.res.Notes...), u.fBody, w-u.px(40))
		}
	}
	steps := pr.steps
	if len(steps) > 5 {
		steps = steps[len(steps)-5:]
	}
	lh := u.fSmall.height + u.px(4)
	bh := u.fBody.height + u.px(2)
	h := u.px(78) + len(steps)*lh + len(body)*bh
	accent := colYellow
	if pr.done && pr.err != "" {
		accent = colRed
	}
	x, y, bot := u.box(c, w, h, pr.title, accent)
	y += u.px(10)
	spin := "|/-\\"[u.frame/2%4 : u.frame/2%4+1]
	for i, st := range steps {
		last := i == len(steps)-1
		mark, col := "✓ ", colPaperDm
		if last && !pr.done {
			mark, col = spin+" ", colPaper
		} else if last && pr.err != "" {
			mark, col = "!! ", colRed
		}
		c.Text(u.fSmall, x+u.px(20), y, mark+u.fSmall.Ellipsize(st, w-u.px(60)), col)
		y += lh
	}
	y += u.px(6)
	for _, l := range body {
		col := colPaper
		if pr.err != "" {
			col = colPaper
		}
		c.Text(u.fBody, x+u.px(20), y, l, col)
		y += bh
	}
	if pr.done {
		c.TextRight(u.fSmall, x+w-u.px(16), bot-u.fSmall.height-u.px(10), "A OK", colPaperDm)
	} else {
		c.TextRight(u.fSmall, x+w-u.px(16), bot-u.fSmall.height-u.px(10), "ELAPSED "+fmt.Sprint(int(time.Since(pr.started).Seconds()))+"S", colPaperFt)
	}
}
