package ui

// 配色与小部件的测试：头像颜色稳定且避开蓝紫、皮肤随系统外观切换、
// 卡片上的来源文案。

import (
	"fmt"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// hueSat 返回颜色的色相（度，0–360）与 HSV 饱和度（0–1）。
func hueSat(c myui.Color) (hue, sat float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	max, min := r, r
	for _, v := range []float64{g, b} {
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	d := max - min
	if max == 0 || d == 0 {
		return 0, 0
	}
	switch max {
	case r:
		hue = 60 * ((g - b) / d)
		if hue < 0 {
			hue += 360
		}
	case g:
		hue = 60 * ((b-r)/d + 2)
	default:
		hue = 60 * ((r-g)/d + 4)
	}
	return hue, d / max
}

// TestAvatarColorIsStableAndSpread 验证同名技能总是同一个头像色、颜色来自
// 色板，且不同名字能分散到多种颜色。
func TestAvatarColorIsStableAndSpread(t *testing.T) {
	for name, sk := range map[string]*skin{"light": kuraScheme.light, "dark": kuraScheme.dark} {
		seen := map[myui.Color]bool{}
		for i := 0; i < 40; i++ {
			skill := fmt.Sprintf("skill-%d", i)
			c := avatarColor(sk.pal, skill)
			if c != avatarColor(sk.pal, skill) {
				t.Fatalf("%s: %s 的头像色应稳定", name, skill)
			}
			in := false
			for _, p := range sk.pal.avatars {
				in = in || p == c
			}
			if !in {
				t.Fatalf("%s: %s 的头像色不在色板里", name, skill)
			}
			seen[c] = true
		}
		if len(seen) < len(sk.pal.avatars)-1 {
			t.Errorf("%s: 40 个名字只用到 %d/%d 种头像色，分布太集中", name, len(seen), len(sk.pal.avatars))
		}
	}
}

// TestAvatarPaletteAvoidsBlueViolet 守住一个明确的审美决定：头像色板里不能
// 有蓝紫色（色相 215°–300° 且有一定饱和度），不和强调色抢戏。偏灰的石板
// 色（低饱和）不受限。
func TestAvatarPaletteAvoidsBlueViolet(t *testing.T) {
	for name, sk := range map[string]*skin{"light": kuraScheme.light, "dark": kuraScheme.dark} {
		for _, c := range sk.pal.avatars {
			h, s := hueSat(c)
			if h >= 215 && h <= 300 && s > 0.3 {
				t.Errorf("%s: 头像色 #%02x%02x%02x 偏蓝紫（色相 %.0f° 饱和度 %.2f）", name, c.R, c.G, c.B, h, s)
			}
		}
	}
}

// TestAvatarInitial 验证头像里的字：英文大写首字母、汉字原样、空名字用问号。
func TestAvatarInitial(t *testing.T) {
	for in, want := range map[string]string{"notes-api": "N", "Tdd": "T", "写作助手": "写", "": "?", "9lives": "9"} {
		if got := avatarInitial(in); got != want {
			t.Errorf("avatarInitial(%q) = %q，想要 %q", in, got, want)
		}
	}
}

// TestSchemeFollowsSystemAppearance 验证皮肤随系统的浅色/深色切换，深色用
// 参考图的底色与卡片色。
func TestSchemeFollowsSystemAppearance(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	tt := showApp(a, 980, 640)

	tt.SetDark(true)
	refresh(a, tt)
	if a.pal.canvas != myui.Hex("#1a1a1e") || a.pal.card != myui.Hex("#232327") || a.pal.line != myui.Hex("#333339") {
		t.Errorf("深色皮肤应取参考图的底色、卡片色与描边：%+v", a.pal)
	}

	tt.SetDark(false)
	refresh(a, tt)
	if a.pal.canvas != kuraScheme.light.pal.canvas || a.pal.canvas == kuraScheme.dark.pal.canvas {
		t.Errorf("切回浅色后应换成浅色皮肤：%+v", a.pal)
	}
}

// TestCardSource 验证卡片名字下的来源文案。
func TestCardSource(t *testing.T) {
	gh := &store.Skill{Meta: &store.Meta{Source: store.Source{Type: store.SourceGitHub, URL: "https://github.com/mattpocock/skills.git"}}}
	local := &store.Skill{Meta: &store.Meta{Source: store.Source{Type: store.SourceLocal, Path: "/x"}}}
	unknown := &store.Skill{Meta: &store.Meta{}}
	for _, c := range []struct {
		name string
		s    *store.Skill
		want string
	}{
		{"github", gh, "mattpocock/skills"},
		{"local", local, "本地"},
		{"unknown", unknown, "未知来源"},
		{"no meta", &store.Skill{}, "无元数据"},
		{"broken meta", &store.Skill{MetaErr: "bad json"}, "元数据损坏"},
	} {
		if got := cardSource(c.s); got != c.want {
			t.Errorf("%s: cardSource = %q，想要 %q", c.name, got, c.want)
		}
	}
}

func TestShortTime(t *testing.T) {
	for in, want := range map[string]string{
		"2026-02-02T13:34:08Z": "2026-02-02 13:34",
		"not a time":           "not a time",
		"":                     "",
	} {
		if got := shortTime(in); got != want {
			t.Errorf("shortTime(%q) = %q，想要 %q", in, got, want)
		}
	}
}
