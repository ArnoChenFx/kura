package ui

// 窗口变窄时两侧栏的让位（PR #4 评审缺陷 6）：已经拖宽的侧栏要按当帧可用
// 宽度夹紧，中栏不能被挤没；窗口再变宽时用户拖出的宽度偏好恢复。

import "testing"

// TestNarrowingWindowClampsDraggedSidebars 验证：先把两侧栏拖到上限，再把窗口
// 缩到 880，中栏至少还有 mainMinW、详情栏不被右边缘裁掉；连续帧不抖；窗口
// 变宽后偏好恢复。
func TestNarrowingWindowClampsDraggedSidebars(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "第一个技能", nil)
	tt := showApp(a, 1600, 800)
	const y = 400

	dragH(a, tt, a.rootsW+dividerWidth/2.0, y, 1000)
	if a.rootsW != rootsMaxW {
		t.Fatalf("前置：左栏应拖到上限 %v，得到 %v", rootsMaxW, a.rootsW)
	}
	click(t, a, tt, "alpha")
	dragH(a, tt, float32(1600)-a.detailW-dividerWidth/2.0, y, -1000)
	if a.detailW != detailMaxW {
		t.Fatalf("前置：详情栏应拖到上限 %v，得到 %v", detailMaxW, a.detailW)
	}

	tt.SetSize(880, 800)
	refresh(a, tt)
	if got := a.rootsEff + a.detailEff + 2*dividerWidth + mainMinW; got > 880 {
		t.Errorf("880 宽时 左栏 %v + 详情 %v + 分割线 + 中栏最小宽 %d = %v，超出窗口", a.rootsEff, a.detailEff, mainMinW, got)
	}
	if a.rootsEff < rootsMinW || a.detailEff < detailMinW {
		t.Errorf("让位不应低于下限：左栏 %v、详情 %v", a.rootsEff, a.detailEff)
	}
	// 真实布局：详情栏的按钮不能被右边缘裁掉，卡片在左栏右边。
	if r, ok := tt.Find("关闭详情"); !ok || r.X+r.W > 880 {
		t.Errorf("详情栏应完整留在窗口内，关闭详情 = %v, %v", r, ok)
	}
	if r, ok := tt.Find("alpha"); !ok || r.X < a.rootsEff {
		t.Errorf("卡片应在左栏右边的中栏里，alpha = %v, %v", r, ok)
	}

	// 连续帧结果相同：夹紧后不会来回抖动。
	r1, d1 := a.rootsEff, a.detailEff
	tt.Frame()
	tt.Frame()
	if a.rootsEff != r1 || a.detailEff != d1 {
		t.Errorf("连续帧宽度抖动：%v/%v → %v/%v", r1, d1, a.rootsEff, a.detailEff)
	}
	if a.rootsW != rootsMaxW || a.detailW != detailMaxW {
		t.Errorf("夹紧不应改用户偏好，得到 %v/%v", a.rootsW, a.detailW)
	}

	// 窗口变宽：偏好恢复。
	tt.SetSize(1600, 800)
	refresh(a, tt)
	if a.rootsEff != rootsMaxW || a.detailEff != detailMaxW {
		t.Errorf("窗口变宽后应恢复用户拖出的宽度，得到 %v/%v", a.rootsEff, a.detailEff)
	}
}

// TestFitWidths 直接测夹紧函数：偏好放得下时原样，放不下时两侧一起让位，
// 详情栏隐藏时左栏用掉余量，且是纯函数（重复调用同一结果）。
func TestFitWidths(t *testing.T) {
	cases := []struct {
		name                  string
		avail, r, d           float32
		detail                bool
		wantRoots, wantDetail float32
	}{
		{name: "放得下", avail: 1600, r: 300, d: 400, detail: true, wantRoots: 300, wantDetail: 400},
		{name: "无窗口宽度只夹上下限", avail: 0, r: 999, d: 10, detail: true, wantRoots: rootsMaxW, wantDetail: detailMinW},
		{name: "隐藏详情左栏用掉余量", avail: 700, r: 420, d: 320, detail: false, wantRoots: 700 - mainMinW - dividerWidth, wantDetail: 320},
		{name: "窗口极窄到下限", avail: 500, r: 420, d: 560, detail: true, wantRoots: rootsMinW, wantDetail: detailMinW},
	}
	for _, c := range cases {
		gr, gd := fitWidths(c.avail, c.r, c.d, c.detail)
		if gr != c.wantRoots || gd != c.wantDetail {
			t.Errorf("%s: fitWidths(%v,%v,%v,%v) = %v/%v，想要 %v/%v", c.name, c.avail, c.r, c.d, c.detail, gr, gd, c.wantRoots, c.wantDetail)
		}
		if r2, d2 := fitWidths(c.avail, c.r, c.d, c.detail); r2 != gr || d2 != gd {
			t.Errorf("%s: 不是纯函数", c.name)
		}
	}
	// 一起让位：两边都比下限宽时都变窄，总和正好放进余量。
	gr, gd := fitWidths(880, 420, 560, true)
	if gr >= 420 || gd >= 560 {
		t.Errorf("两侧应一起让位，得到 %v/%v", gr, gd)
	}
	if sum := gr + gd + 2*dividerWidth + mainMinW; sum > 880.01 || sum < 879.99 {
		t.Errorf("让位后应正好放进窗口，总和 %v", sum)
	}
}
