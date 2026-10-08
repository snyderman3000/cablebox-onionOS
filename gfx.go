package main

// Software drawing on an RGBA canvas plus cached TrueType text (shared with Rewind and Mixtape).

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/body.ttf
var bodyTTF []byte

//go:embed assets/title.ttf
var titleTTF []byte

//go:embed assets/lcd.ttf
var lcdTTF []byte

// Palette: an 80s cable converter box (woodgrain, dark face, red LEDs) over a
// navy program-guide channel, with the family's beige keycaps.
var (
	colBG     = rgb(0x04, 0x0a, 0x06) // CRT glass
	colCRT2   = rgb(0x0a, 0x18, 0x0e) // raised glass: panels, selected rows
	colPhos   = rgb(0x5c, 0xff, 0x86) // lit phosphor
	colPhosMd = rgb(0x34, 0xb8, 0x5a) // secondary text
	colPhosDm = rgb(0x1c, 0x62, 0x31) // labels, rules
	colPhosFt = rgb(0x10, 0x30, 0x1a) // faint grid
	colAmber  = rgb(0xff, 0xb4, 0x2e) // warnings, updates
	colRed    = rgb(0xff, 0x45, 0x38) // down, errors
	colLampG  = rgb(0x6a, 0xff, 0x70)
	colLampA  = rgb(0xff, 0xb0, 0x30)
	colLampR  = rgb(0xff, 0x3a, 0x2c)
	colLampW  = rgb(0xff, 0xf2, 0xd0)
	colEnamel = rgb(0x3c, 0x52, 0x6c) // cabinet paint
	colEnamHi = rgb(0x5e, 0x77, 0x94)
	colEnamDk = rgb(0x22, 0x30, 0x42)
	colStencl = rgb(0xec, 0xe6, 0xd6) // white stencil lettering
	colCap    = rgb(0xdc, 0xd3, 0xbe) // keycap beige
	colCapDk  = rgb(0xa6, 0x9c, 0x86)
	colCapTxt = rgb(0x2c, 0x28, 0x22)
	colKeybed = rgb(0x2e, 0x2a, 0x26)
	colCase   = rgb(0xde, 0xd5, 0xbf) // modem shell
	colCaseDk = rgb(0xbc, 0xb2, 0x9a)
	colLED    = rgb(0xff, 0x2c, 0x1c) // red indicator LEDs

	// the program guide: navy grid cells, white listings, yellow highlights
	colGuide   = rgb(0x0c, 0x16, 0x5c)
	colGuide2  = rgb(0x06, 0x0c, 0x38)
	colCell    = rgb(0x1a, 0x2a, 0x92)
	colCellHi  = rgb(0x2c, 0x42, 0xc4)
	colCellLn  = rgb(0x5a, 0x72, 0xe6)
	colYellow  = rgb(0xff, 0xe2, 0x3c)
	colPaper   = rgb(0xf2, 0xf0, 0xe6)
	colPaperDm = rgb(0xa8, 0xb4, 0xe8)
	colPaperFt = rgb(0x6c, 0x7c, 0xc8)
	colTomato  = rgb(0xfa, 0x32, 0x0a)
	colSplat   = rgb(0x6c, 0xc3, 0x2a)
	colGreenOK = rgb(0x6a, 0xf0, 0x6a)
	colOrange  = rgb(0xff, 0x9a, 0x2a)
	colWood    = rgb(0x6a, 0x42, 0x24)
	colWoodDk  = rgb(0x3e, 0x24, 0x12)
	colFace    = rgb(0x1c, 0x18, 0x16) // the cable box's dark face
	colBlack   = rgb(0, 0, 0)
	colWhite   = rgb(0xf4, 0xf4, 0xf0)
)

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

func mix(a, b color.RGBA, t float64) color.RGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}

type Canvas struct {
	*image.RGBA
	W, H int
}

func NewCanvas(w, h int) *Canvas {
	return &Canvas{RGBA: image.NewRGBA(image.Rect(0, 0, w, h)), W: w, H: h}
}

func (c *Canvas) Fill(x, y, w, h int, col color.RGBA) {
	r := image.Rect(x, y, x+w, y+h).Intersect(c.Rect)
	if r.Empty() {
		return
	}
	// fast row fill
	p := c.Pix
	st := c.Stride
	row := make([]byte, r.Dx()*4)
	for i := 0; i < len(row); i += 4 {
		row[i], row[i+1], row[i+2], row[i+3] = col.R, col.G, col.B, 255
	}
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		o := yy*st + r.Min.X*4
		copy(p[o:o+len(row)], row)
	}
}

// Blend fills a rectangle blending col over existing pixels with alpha a (0..1).
func (c *Canvas) Blend(x, y, w, h int, col color.RGBA, a float64) {
	r := image.Rect(x, y, x+w, y+h).Intersect(c.Rect)
	ia := int(a * 256)
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		o := yy*c.Stride + r.Min.X*4
		for xx := r.Min.X; xx < r.Max.X; xx++ {
			c.Pix[o] = uint8((int(c.Pix[o])*(256-ia) + int(col.R)*ia) >> 8)
			c.Pix[o+1] = uint8((int(c.Pix[o+1])*(256-ia) + int(col.G)*ia) >> 8)
			c.Pix[o+2] = uint8((int(c.Pix[o+2])*(256-ia) + int(col.B)*ia) >> 8)
			o += 4
		}
	}
}

func (c *Canvas) Set(x, y int, col color.RGBA) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return
	}
	o := y*c.Stride + x*4
	c.Pix[o], c.Pix[o+1], c.Pix[o+2], c.Pix[o+3] = col.R, col.G, col.B, 255
}

func (c *Canvas) Border(x, y, w, h, t int, col color.RGBA) {
	c.Fill(x, y, w, t, col)
	c.Fill(x, y+h-t, w, t, col)
	c.Fill(x, y, t, h, col)
	c.Fill(x+w-t, y, t, h, col)
}

// Corners draws HUD-style corner brackets.
func (c *Canvas) Corners(x, y, w, h, l int, col color.RGBA) {
	c.Fill(x, y, l, 2, col)
	c.Fill(x, y, 2, l, col)
	c.Fill(x+w-l, y, l, 2, col)
	c.Fill(x+w-2, y, 2, l, col)
	c.Fill(x, y+h-2, l, 2, col)
	c.Fill(x, y+h-l, 2, l, col)
	c.Fill(x+w-l, y+h-2, l, 2, col)
	c.Fill(x+w-2, y+h-l, 2, l, col)
}

func (c *Canvas) HLineDashed(x, y, w, dash, gap int, col color.RGBA) {
	for i := x; i < x+w; i += dash + gap {
		n := dash
		if i+n > x+w {
			n = x + w - i
		}
		c.Fill(i, y, n, 1, col)
	}
}

func (c *Canvas) Circle(cx, cy, r int, col color.RGBA, filled bool, thick int) {
	r2 := float64(r) * float64(r)
	in := float64(r-thick) * float64(r-thick)
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			d := float64(x*x + y*y)
			if d <= r2 && (filled || d >= in) {
				c.Set(cx+x, cy+y, col)
			}
		}
	}
}

// Line with Bresenham, thickness 1.
func (c *Canvas) Line(x0, y0, x1, y1 int, col color.RGBA) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		c.Set(x0, y0, col)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Scanlines darkens every other row — the CRT look. Applied to a region.
func (c *Canvas) Scanlines(x, y, w, h int, strength float64) {
	r := image.Rect(x, y, x+w, y+h).Intersect(c.Rect)
	k := int((1 - strength) * 256)
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		if yy%2 == 0 {
			continue
		}
		o := yy*c.Stride + r.Min.X*4
		for xx := r.Min.X; xx < r.Max.X; xx++ {
			c.Pix[o] = uint8(int(c.Pix[o]) * k >> 8)
			c.Pix[o+1] = uint8(int(c.Pix[o+1]) * k >> 8)
			c.Pix[o+2] = uint8(int(c.Pix[o+2]) * k >> 8)
			o += 4
		}
	}
}

// DrawImage scales src into the destination rect (nearest-neighbour).
func (c *Canvas) DrawImage(src *image.RGBA, x, y, w, h int) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == 0 || sh == 0 {
		return
	}
	for dy := 0; dy < h; dy++ {
		py := y + dy
		if py < 0 || py >= c.H {
			continue
		}
		sy := sb.Min.Y + dy*sh/h
		for dx := 0; dx < w; dx++ {
			px := x + dx
			if px < 0 || px >= c.W {
				continue
			}
			sx := sb.Min.X + dx*sw/w
			so := (sy-sb.Min.Y)*src.Stride + (sx-sb.Min.X)*4
			do := py*c.Stride + px*4
			c.Pix[do], c.Pix[do+1], c.Pix[do+2], c.Pix[do+3] = src.Pix[so], src.Pix[so+1], src.Pix[so+2], 255
		}
	}
}

// ---------- text ----------

type glyph struct {
	mask    *image.Alpha
	off     image.Point
	advance int
}

type Font struct {
	face    font.Face
	cache   map[rune]*glyph
	ascent  int
	height  int
	fixedSp bool
}

func loadFont(ttf []byte, size float64) *Font {
	f, err := opentype.Parse(ttf)
	if err != nil {
		panic(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	m := face.Metrics()
	return &Font{face: face, cache: map[rune]*glyph{}, ascent: m.Ascent.Ceil(), height: m.Height.Ceil()}
}

func (f *Font) glyph(r rune) *glyph {
	if g, ok := f.cache[r]; ok {
		return g
	}
	if g := f.symbol(r); g != nil {
		f.cache[r] = g
		return g
	}
	dr, mask, mp, adv, ok := f.face.Glyph(fixed.P(0, 0), r)
	if !ok {
		if r != '?' {
			g := f.glyph('?')
			f.cache[r] = g
			return g
		}
		f.cache[r] = &glyph{advance: adv.Round()}
		return f.cache[r]
	}
	a := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	draw.Draw(a, a.Bounds(), mask, mp, draw.Src)
	g := &glyph{mask: a, off: dr.Min, advance: adv.Round()}
	f.cache[r] = g
	return g
}

// symbol synthesises UI glyphs the bundled fonts don't have (triangles, dots, arrows).
func (f *Font) symbol(r rune) *glyph {
	switch r {
	case '▲', '▼', '●', '○', '◀', '▶', '↕', '→', '✓', '★', '⏏', '■', '❚', '↑', '↓':
	default:
		return nil
	}
	s := f.ascent * 7 / 10 // symbol box size
	if s < 6 {
		s = 6
	}
	w := s
	if r == '→' {
		w = s * 3 / 2
	}
	m := image.NewAlpha(image.Rect(0, 0, w, s))
	set := func(x, y int) {
		if x >= 0 && y >= 0 && x < w && y < s {
			m.Pix[y*m.Stride+x] = 255
		}
	}
	c := float64(s-1) / 2
	for y := 0; y < s; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x), float64(y)
			d := math.Hypot(fx-c, fy-c)
			switch r {
			case '▲':
				if math.Abs(fx-c) <= fy/2+0.5 {
					set(x, y)
				}
			case '⏏':
				if (fy < c*1.2 && math.Abs(fx-c) <= fy*0.9/1.2+0.5) || (fy >= c*1.5 && fy < c*1.95) {
					set(x, y)
				}
			case '■':
				if fx >= c*0.25 && fx <= c*1.75 && fy >= c*0.25 && fy <= c*1.75 {
					set(x, y)
				}
			case '❚':
				if math.Abs(fx-c) <= c*0.3 {
					set(x, y)
				}
			case '▼':
				if math.Abs(fx-c) <= (float64(s-1)-fy)/2+0.5 {
					set(x, y)
				}
			case '●':
				if d <= c+0.3 {
					set(x, y)
				}
			case '○':
				if d <= c+0.3 && d >= c-1.6 {
					set(x, y)
				}
			case '◀':
				if math.Abs(fy-c) <= fx/2+0.5 {
					set(x, y)
				}
			case '▶':
				if math.Abs(fy-c) <= (float64(s-1)-fx)/2+0.5 {
					set(x, y)
				}
			case '↑', '↓':
				hy := fy // distance from the arrow's tip
				if r == '↓' {
					hy = float64(s-1) - fy
				}
				if (math.Abs(fx-c) <= 0.8 && hy > 0) || (hy < c*0.9 && math.Abs(fx-c) <= hy+0.5) {
					set(x, y)
				}
			case '↕':
				hy := math.Min(fy, float64(s-1)-fy)
				if math.Abs(fx-c) <= 0.8 || (hy < c*0.7 && math.Abs(fx-c) <= hy+0.5) {
					set(x, y)
				}
			case '→':
				tip := float64(w - 1)
				if (math.Abs(fy-c) <= 0.8 && fx < tip-1) || (fx >= tip-c && math.Abs(fy-c) <= (tip-fx)+0.3) {
					set(x, y)
				}
			case '★':
				// five-point star from polar distance
				a := math.Atan2(fy-c, fx-c) + math.Pi/2
				k := math.Mod(a+2*math.Pi, 2*math.Pi/5) / (2 * math.Pi / 5)
				rr := c * (0.45 + 0.55*math.Abs(1-2*k))
				if d <= rr+0.3 {
					set(x, y)
				}
			case '✓':
				if (x < s/2 && math.Abs(fy-(c+fx*0.6)) <= 1) || (x >= s/2-1 && math.Abs(fy-(float64(s-1)-(fx-float64(s/2))*1.4)) <= 1) {
					set(x, y)
				}
			}
		}
	}
	top := -f.ascent*36/100 - s/2
	return &glyph{mask: m, off: image.Pt(1, top), advance: w + 3}
}

func (f *Font) Width(s string) int {
	w := 0
	for _, r := range s {
		w += f.glyph(r).advance
	}
	return w
}

// Text draws s with its top-left at (x,y). Returns the x after the text.
func (c *Canvas) Text(f *Font, x, y int, s string, col color.RGBA) int {
	base := y + f.ascent
	for _, r := range s {
		g := f.glyph(r)
		if g.mask != nil {
			gx, gy := x+g.off.X, base+g.off.Y
			b := g.mask.Bounds()
			for yy := 0; yy < b.Dy(); yy++ {
				py := gy + yy
				if py < 0 || py >= c.H {
					continue
				}
				for xx := 0; xx < b.Dx(); xx++ {
					px := gx + xx
					if px < 0 || px >= c.W {
						continue
					}
					a := int(g.mask.Pix[yy*g.mask.Stride+xx])
					if a == 0 {
						continue
					}
					o := py*c.Stride + px*4
					c.Pix[o] = uint8((int(c.Pix[o])*(255-a) + int(col.R)*a) / 255)
					c.Pix[o+1] = uint8((int(c.Pix[o+1])*(255-a) + int(col.G)*a) / 255)
					c.Pix[o+2] = uint8((int(c.Pix[o+2])*(255-a) + int(col.B)*a) / 255)
				}
			}
		}
		x += g.advance
	}
	return x
}

// TextGlow draws text with a soft phosphor halo.
func (c *Canvas) TextGlow(f *Font, x, y int, s string, col color.RGBA) int {
	halo := mix(colBG, col, 0.28)
	for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		c.Text(f, x+d[0], y+d[1], s, halo)
	}
	return c.Text(f, x, y, s, col)
}

func (c *Canvas) TextRight(f *Font, right, y int, s string, col color.RGBA) {
	c.Text(f, right-f.Width(s), y, s, col)
}

func (c *Canvas) TextCenter(f *Font, cx, y int, s string, col color.RGBA) {
	c.Text(f, cx-f.Width(s)/2, y, s, col)
}

// Ellipsize trims s to fit width w.
func (f *Font) Ellipsize(s string, w int) string {
	if f.Width(s) <= w {
		return s
	}
	for len(s) > 0 {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
		if f.Width(s+"…") <= w {
			return s + "…"
		}
	}
	return ""
}

// Wrap breaks text into lines no wider than w.
func (f *Font) Wrap(s string, w int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		line := ""
		for _, wd := range words {
			try := wd
			if line != "" {
				try = line + " " + wd
			}
			if f.Width(try) <= w {
				line = try
				continue
			}
			if line != "" {
				out = append(out, line)
			}
			for f.Width(wd) > w { // hard-break very long tokens (URLs)
				cut := len(wd)
				for cut > 1 && f.Width(wd[:cut]) > w {
					cut--
				}
				out = append(out, wd[:cut])
				wd = wd[cut:]
			}
			line = wd
		}
		out = append(out, line)
	}
	return out
}

// Noise fills a region with animated static ("NO SIGNAL").
func (c *Canvas) Noise(x, y, w, h int, seed uint32) {
	s := seed*2654435761 + 1
	for yy := y; yy < y+h; yy += 2 {
		for xx := x; xx < x+w; xx += 2 {
			s ^= s << 13
			s ^= s >> 17
			s ^= s << 5
			v := uint8(s>>24) / 3
			col := rgb(v, v, v+v/4)
			c.Set(xx, yy, col)
			c.Set(xx+1, yy, col)
			c.Set(xx, yy+1, col)
			c.Set(xx+1, yy+1, col)
		}
	}
}
