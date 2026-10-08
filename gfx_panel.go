package main

// Carrier's widgets: the beige modem case and its LEDs, lamps, seven-segment
// readouts, keycaps and QR codes. Shared lineage with Mainframe.

import (
	"image/color"
	"math"
	"strings"

	"cablebox/internal/qr"
)

func (c *Canvas) VGrad(x, y, w, h int, top, bot color.RGBA) {
	for i := 0; i < h; i++ {
		c.Fill(x, y+i, w, 1, mix(top, bot, float64(i)/float64(max(1, h-1))))
	}
}

// BlendCircle tints a disc with col at alpha a.
func (c *Canvas) BlendCircle(cx, cy, r int, col color.RGBA, a float64) {
	r2 := r * r
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r2 {
				c.Blend(cx+x, cy+y, 1, 1, col, a)
			}
		}
	}
}

// Enamel draws a painted cabinet panel with a lit top edge and a shadowed bottom.
func (c *Canvas) Enamel(x, y, w, h int) {
	c.VGrad(x, y, w, h, colEnamel, mix(colEnamel, colEnamDk, 0.7))
	c.Fill(x, y, w, 1, colEnamHi)
	c.Fill(x, y+h-1, w, 1, colEnamDk)
	// faint orange-peel texture
	s := uint32(x*31 + y*17 + 7)
	for yy := y + 1; yy < y+h-1; yy += 3 {
		for xx := x + (yy/3)%3; xx < x+w; xx += 5 {
			s ^= s << 13
			s ^= s >> 17
			s ^= s << 5
			if s%3 == 0 {
				c.Blend(xx, yy, 1, 1, colBlack, 0.08)
			}
		}
	}
}

// Screw draws a slotted panel screw.
func (c *Canvas) Screw(cx, cy, r int) {
	c.Circle(cx, cy, r, colEnamDk, true, 0)
	c.Circle(cx, cy, r-1, rgb(0x9a, 0xa2, 0xae), true, 0)
	c.Line(cx-r+2, cy+1, cx+r-2, cy-1, rgb(0x40, 0x46, 0x50))
}

// Lamp draws a round indicator lamp: a dark bezel with a coloured lens that
// glows when lit. glow (0..1) dims it, for flicker.
func (c *Canvas) Lamp(cx, cy, r int, col color.RGBA, on bool, glow float64) {
	c.Circle(cx, cy, r+1, rgb(0x12, 0x14, 0x18), true, 0)
	if !on {
		c.Circle(cx, cy, r-1, mix(rgb(0x18, 0x1a, 0x1e), col, 0.18), true, 0)
		c.Set(cx-r/3, cy-r/3, mix(rgb(0x18, 0x1a, 0x1e), colWhite, 0.25))
		return
	}
	glow = math.Max(0.25, math.Min(1, glow))
	c.BlendCircle(cx, cy, r+max(2, r/2), col, 0.18*glow)
	c.Circle(cx, cy, r-1, mix(mix(col, colBlack, 0.35), col, glow), true, 0)
	hot := max(1, r/2)
	c.Circle(cx, cy, hot, mix(col, colLampW, 0.55*glow), true, 0)
	c.Set(cx-r/3, cy-r/3, colWhite)
}

// Case draws the modem's beige plastic shell with a moulded top edge.
func (c *Canvas) Case(x, y, w, h int) {
	c.VGrad(x, y, w, h, colCase, colCaseDk)
	c.Fill(x, y, w, 1, mix(colCase, colWhite, 0.5))
	c.Fill(x, y+h-1, w, 1, rgb(0x6e, 0x66, 0x56))
	// fine moulding texture
	s := uint32(x*13 + y*7 + 3)
	for yy := y + 1; yy < y+h-1; yy += 2 {
		for xx := x + (yy/2)%4; xx < x+w; xx += 6 {
			s ^= s << 13
			s ^= s >> 17
			s ^= s << 5
			if s%4 == 0 {
				c.Blend(xx, yy, 1, 1, colBlack, 0.05)
			}
		}
	}
}

// LED draws a small rectangular indicator LED, like the row on a 1980s modem.
func (c *Canvas) LED(x, y, w, h int, col color.RGBA, on bool, glow float64) {
	c.Fill(x-1, y-1, w+2, h+2, rgb(0x0a, 0x0a, 0x0a))
	if !on {
		c.Fill(x, y, w, h, mix(rgb(0x1a, 0x0c, 0x0a), col, 0.16))
		c.Fill(x, y, w, 1, mix(rgb(0x1a, 0x0c, 0x0a), colWhite, 0.12))
		return
	}
	glow = math.Max(0.3, math.Min(1, glow))
	c.Blend(x-3, y-3, w+6, h+6, col, 0.16*glow)
	c.Fill(x, y, w, h, mix(mix(col, colBlack, 0.4), col, glow))
	c.Fill(x+1, y+1, w-2, max(1, h/3), mix(col, colLampW, 0.5*glow))
}

// Keycap draws a beige sculpted keycap.
func (c *Canvas) Keycap(x, y, w, h int) {
	c.Fill(x, y, w, h, colCapDk)
	c.Fill(x+1, y+1, w-2, h-2, mix(colCap, colCapDk, 0.35))
	c.Fill(x+3, y+2, w-6, h-6, colCap)
	c.Fill(x+3, y+2, w-6, 1, mix(colCap, colWhite, 0.5))
}

// Seg7 draws digits (and . : - %) as seven-segment glyphs, unlit segments faintly visible.
func (c *Canvas) Seg7(x, y, dw, dh, t int, s string, on, bg color.RGBA) int {
	segs := map[rune]string{'0': "abcdef", '1': "bc", '2': "abged", '3': "abgcd", '4': "fgbc", '5': "afgcd",
		'6': "afgedc", '7': "abc", '8': "abcdefg", '9': "abcdfg", '-': "g", ' ': ""}
	off := mix(bg, on, 0.09)
	glow := mix(bg, on, 0.30)
	for _, r := range s {
		switch r {
		case ':':
			c.Fill(x+t, y+dh/3-t/2, t, t, on)
			c.Fill(x+t, y+dh*2/3-t/2, t, t, on)
			x += t*3 + 1
			continue
		case '.':
			c.Fill(x, y+dh-t, t, t, on)
			x += t * 2
			continue
		}
		lit := segs[r]
		h2 := dh / 2
		rects := map[byte][4]int{
			'a': {x + t, y, dw - 2*t, t},
			'b': {x + dw - t, y + t, t, h2 - t},
			'c': {x + dw - t, y + h2, t, h2 - t},
			'd': {x + t, y + dh - t, dw - 2*t, t},
			'e': {x, y + h2, t, h2 - t},
			'f': {x, y + t, t, h2 - t},
			'g': {x + t, y + h2 - t/2, dw - 2*t, t},
		}
		for _, k := range []byte("abcdefg") {
			rr := rects[k]
			if strings.IndexByte(lit, k) >= 0 {
				c.Blend(rr[0]-1, rr[1]-1, rr[2]+2, rr[3]+2, glow, 0.6)
				c.Fill(rr[0], rr[1], rr[2], rr[3], on)
			} else {
				c.Fill(rr[0], rr[1], rr[2], rr[3], off)
			}
		}
		x += dw + t*2
	}
	return x
}

func Seg7Width(dw, t int, s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ':':
			w += t*3 + 1
		case '.':
			w += t * 2
		default:
			w += dw + t*2
		}
	}
	return w
}

// SegBar is a row of level segments, lit fraction v, on background bg.
func (c *Canvas) SegBar(x, y, w, h, n int, v float64, on, bg color.RGBA) {
	gap := max(1, w/(n*5))
	sw := (w - gap*(n-1)) / n
	lit := int(math.Round(math.Max(0, math.Min(1, v)) * float64(n)))
	if v > 0 && lit == 0 {
		lit = 1
	}
	for i := 0; i < n; i++ {
		col := on
		if i >= lit {
			col = mix(bg, on, 0.13)
		}
		c.Fill(x+i*(sw+gap), y, sw, h, col)
	}
}

// QR draws text as a QR code with a white quiet zone, module px per cell. Returns the size drawn.
func (c *Canvas) QR(x, y, module int, text string) int {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return 0
	}
	n := code.Size + 8
	c.Fill(x, y, n*module, n*module, rgb(0xff, 0xff, 0xff))
	for yy := 0; yy < code.Size; yy++ {
		for xx := 0; xx < code.Size; xx++ {
			if code.Black(xx, yy) {
				c.Fill(x+(xx+4)*module, y+(yy+4)*module, module, module, colBlack)
			}
		}
	}
	return n * module
}

func QRSize(text string, module int) int {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return 0
	}
	return (code.Size + 8) * module
}
