package main

// Searching by name: an on-screen keyboard that tunes CH 01 to the results.

import (
	"fmt"
	"strings"
)

var kbRows = []string{"ABCDEFGHIJ", "KLMNOPQRST", "UVWXYZ0123", "456789'&-:"}

// the wide keys on the bottom row, with the columns each one covers
var kbSpecial = []struct {
	label      string
	start, end int
}{{"SPACE", 0, 2}, {"DELETE", 3, 4}, {"CLEAR", 5, 6}, {"SEARCH", 7, 9}}

type keyboardState struct {
	text     string
	row, col int // row len(kbRows) is the special row; rows after it are recent searches
}

func (u *UI) openKeyboard() {
	u.kb = keyboardState{text: u.guide.Search}
	u.scr = scrKeyboard
}

func (u *UI) kbRowCount() int { return len(kbRows) + 1 + len(u.guide.Recent) }

func specialAt(col int) int {
	for i, k := range kbSpecial {
		if col >= k.start && col <= k.end {
			return i
		}
	}
	return 0
}

func (u *UI) keyKeyboard(k int) {
	kb := &u.kb
	rows := u.kbRowCount()
	switch k {
	case BtnUp:
		kb.row = (kb.row - 1 + rows) % rows
	case BtnDown:
		kb.row = (kb.row + 1) % rows
	case BtnLeft, BtnRight:
		d := 1
		if k == BtnLeft {
			d = -1
		}
		switch {
		case kb.row < len(kbRows):
			kb.col = (kb.col + d + 10) % 10
		case kb.row == len(kbRows):
			i := (specialAt(kb.col) + d + len(kbSpecial)) % len(kbSpecial)
			kb.col = kbSpecial[i].start
		}
	case BtnA:
		switch {
		case kb.row < len(kbRows):
			if len(kb.text) < 40 {
				kb.text += string(kbRows[kb.row][kb.col])
			}
		case kb.row == len(kbRows):
			switch kbSpecial[specialAt(kb.col)].label {
			case "SPACE":
				u.kbType(" ")
			case "DELETE":
				u.kbDelete()
			case "CLEAR":
				kb.text = ""
			case "SEARCH":
				u.runSearch(kb.text)
			}
		default:
			u.runSearch(u.guide.Recent[kb.row-len(kbRows)-1])
		}
	case BtnX:
		u.kbType(" ")
	case BtnB:
		if kb.text == "" {
			u.scr = scrGuide
			return
		}
		u.kbDelete()
	case BtnStart:
		u.runSearch(kb.text)
	case BtnSelect:
		u.scr = scrGuide
	}
}

func (u *UI) kbType(s string) {
	if len(u.kb.text) < 40 && !(s == " " && (u.kb.text == "" || strings.HasSuffix(u.kb.text, " "))) {
		u.kb.text += s
	}
}

func (u *UI) kbDelete() {
	if n := len(u.kb.text); n > 0 {
		u.kb.text = u.kb.text[:n-1]
	}
}

// runSearch tunes CH 01 to the shows matching q.
func (u *UI) runSearch(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		u.showToast("TYPE A SHOW'S NAME FIRST")
		return
	}
	u.guide.Search = q
	u.guide.AddRecent(q)
	delete(u.guide.Lists, "search")
	u.dirty = true
	u.scr = scrGuide
	for i, ch := range u.lineup {
		if ch.Kind == chSearch {
			u.tune(i)
			return
		}
	}
}

func (u *UI) drawKeyboard(c *Canvas) {
	top := u.topH()
	c.Fill(0, top, c.W, u.headH(), colGuide2)
	c.Fill(0, top+u.headH()-1, c.W, 1, colCellLn)
	tag := "CH 01"
	tw := u.fLCD.Width(tag) + u.px(16)
	c.Fill(u.px(10), top+u.px(4), tw, u.headH()-u.px(8), colYellow)
	c.Text(u.fLCD, u.px(18), top+u.px(4), tag, colGuide2)
	c.Text(u.fBig, u.px(18)+tw, top+u.px(8), "SEARCH BY NAME", colPaper)

	// the text being typed
	y := top + u.headH() + u.px(10)
	fx, fw, fh := u.px(14), c.W-u.px(28), u.fLCD.height+u.px(10)
	c.Fill(fx, y, fw, fh, colGuide2)
	c.Border(fx, y, fw, fh, 2, colCellLn)
	t := u.fLCD.Ellipsize(u.kb.text, fw-u.px(30))
	x := c.Text(u.fLCD, fx+u.px(10), y+u.px(5), t, colPaper)
	if u.frame/6%2 == 0 {
		c.Fill(x+u.px(2), y+u.px(6), u.px(10), u.fLCD.height-u.px(2), colYellow)
	}
	if u.kb.text == "" {
		c.Text(u.fSmall, x+u.px(18), y+u.px(9), "a show's name, or part of it", colPaperFt)
	}
	y += fh + u.px(10)

	// keys
	kw := (c.W - u.px(28)) / 10
	kh := u.px(30)
	for r, row := range kbRows {
		for col, ch := range row {
			sel := u.kb.row == r && u.kb.col == col
			kx := u.px(14) + col*kw
			u.cell(c, kx, y, kw-u.px(4), kh-u.px(4), sel)
			fg := colPaper
			if sel {
				fg = colGuide2
			}
			c.TextCenter(u.fLCD, kx+(kw-u.px(4))/2, y+u.px(1), string(ch), fg)
		}
		y += kh
	}
	for i, k := range kbSpecial {
		sel := u.kb.row == len(kbRows) && specialAt(u.kb.col) == i
		kx := u.px(14) + k.start*kw
		w := (k.end-k.start+1)*kw - u.px(4)
		u.cell(c, kx, y, w, kh-u.px(4), sel)
		fg := colPaper
		if k.label == "SEARCH" && !sel {
			fg = colYellow
		}
		if sel {
			fg = colGuide2
		}
		c.TextCenter(u.fSmall, kx+w/2, y+u.px(5), k.label, fg)
	}
	y += kh + u.px(4)

	// recent searches
	if len(u.guide.Recent) > 0 {
		c.Text(u.fTiny, u.px(16), y, "RECENT", colPaperFt)
		y += u.fTiny.height + u.px(2)
		rh := u.fSmall.height + u.px(6)
		for i, q := range u.guide.Recent {
			if y+rh > u.contentBot()-u.px(2) {
				break
			}
			sel := u.kb.row == len(kbRows)+1+i
			if sel {
				c.Fill(u.px(14), y-u.px(2), c.W-u.px(28), rh, colYellow)
			}
			fg := colPaperDm
			if sel {
				fg = colGuide2
			}
			c.Text(u.fSmall, u.px(22), y+u.px(1), fmt.Sprintf("▶ %s", strings.ToUpper(q)), fg)
			y += rh
		}
	}
	u.keys(c, [][2]string{{"A", "TYPE"}, {"B", "DELETE"}, {"X", "SPACE"}, {"START", "SEARCH"}, {"SELECT", "CANCEL"}})
}
