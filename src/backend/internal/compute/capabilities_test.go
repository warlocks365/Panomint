package compute

import "testing"

// fakeProber 可切换的 NVIDIA 探测器桩，用来覆盖"有卡/无卡"两条分支。
type fakeProber struct {
	available bool
	name      string
	vram      int
}

func (f fakeProber) Available() bool { return f.available }
func (f fakeProber) Info() (string, int) {
	return f.name, f.vram
}

// TestParseDevice -device 取值域：cpu|cuda|auto，大小写与空白宽松，其它报错。
func TestParseDevice(t *testing.T) {
	ok := map[string]string{
		"":       DeviceAuto, // 未指定 = 自动挑
		"auto":   DeviceAuto,
		"AUTO":   DeviceAuto,
		" Auto ": DeviceAuto,
		"cpu":    DeviceCPU,
		"CPU":    DeviceCPU,
		"cuda":   DeviceCUDA,
		"CUDA":   DeviceCUDA,
	}
	for in, want := range ok {
		got, err := ParseDevice(in)
		if err != nil {
			t.Fatalf("ParseDevice(%q) 不应报错: %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseDevice(%q) = %q，期望 %q", in, got, want)
		}
	}
	for _, in := range []string{"gpu", "cudnn", "tpu", "0", "auto2"} {
		if _, err := ParseDevice(in); err == nil {
			t.Fatalf("ParseDevice(%q) 应报错", in)
		}
	}
}

// TestDetectCapabilities 能力探测的四种组合。
//
// 其中「cuda 但无 NVENC → 降级为 CPU 且不报错」是**双接口原则的核心断言**：
// 它保证"开发机没有独显"永远不会变成"软件砍掉 GPU 路径"。
func TestDetectCapabilities(t *testing.T) {
	withNV := fakeProber{available: true, name: "NVIDIA GeForce RTX 3090", vram: 24576}
	withoutNV := fakeProber{available: false}

	t.Run("cpu：不看硬件，直接 CPU 能力", func(t *testing.T) {
		// 即便探测得到 NVENC，显式要求 cpu 时也必须听调用方的。
		c := DetectCapabilities(DeviceCPU, 2, withNV)
		if c.HasNVENC {
			t.Fatal("device=cpu 时 has_nvenc 必须为 false")
		}
		if c.VRAMMB != 0 {
			t.Fatalf("device=cpu 时 vram_mb 应为 0，实际 %d", c.VRAMMB)
		}
		if c.CodecsString() != "h264" {
			t.Fatalf("device=cpu 时 codecs 应仅 h264，实际 %q", c.CodecsString())
		}
		if c.Concurrency != 2 {
			t.Fatalf("并发数应透传，实际 %d", c.Concurrency)
		}
	})

	t.Run("auto + 有 NVENC：上报 GPU 能力", func(t *testing.T) {
		c := DetectCapabilities(DeviceAuto, 1, withNV)
		if !c.HasNVENC {
			t.Fatal("探测到 NVENC 时 has_nvenc 应为 true")
		}
		if c.VRAMMB != 24576 {
			t.Fatalf("vram_mb 应取探测值 24576，实际 %d", c.VRAMMB)
		}
		if c.CodecsString() != "h264,hevc" {
			t.Fatalf("有 NVENC 时 codecs 应含 hevc，实际 %q", c.CodecsString())
		}
	})

	t.Run("auto + 无 NVENC：降级为 CPU，不报错", func(t *testing.T) {
		c := DetectCapabilities(DeviceAuto, 1, withoutNV)
		if c.HasNVENC || c.VRAMMB != 0 || c.CodecsString() != "h264" {
			t.Fatalf("应降级为 CPU 能力，实际 %+v", c)
		}
	})

	t.Run("cuda + 无 NVENC：只降级、不报错、has_nvenc=false（双接口原则）", func(t *testing.T) {
		c := DetectCapabilities(DeviceCUDA, 4, withoutNV)
		if c.HasNVENC {
			t.Fatal("无 NVENC 时 has_nvenc 必须为 false")
		}
		if c.CodecsString() != "h264" {
			t.Fatalf("降级后 codecs 应仅 h264，实际 %q", c.CodecsString())
		}
		if c.Concurrency != 4 {
			t.Fatalf("降级不应丢掉并发配置，实际 %d", c.Concurrency)
		}
		// DetectCapabilities 的签名里没有 error —— 这条断言把这个设计意图钉死：
		// 能力探测不允许"失败"，否则调用方会顺手在无卡机器上退出。
		_ = c
	})

	t.Run("concurrency <= 0 归一为 1", func(t *testing.T) {
		for _, v := range []int{0, -1, -100} {
			if c := DetectCapabilities(DeviceCPU, v, withoutNV); c.Concurrency != 1 {
				t.Fatalf("concurrency=%d 应归一为 1，实际 %d", v, c.Concurrency)
			}
		}
	})

	t.Run("非法 device：按 CPU 处理（探测不阻断启动）", func(t *testing.T) {
		c := DetectCapabilities("bogus", 1, withNV)
		if c.HasNVENC {
			t.Fatal("非法 device 不应判为有 NVENC")
		}
	})

	t.Run("prober 为 nil 时回落到真实探测器而不 panic", func(t *testing.T) {
		// 本机可能没有 nvidia-smi，但无论有没有都不允许 panic。
		_ = DetectCapabilities(DeviceAuto, 1, nil)
	})
}

// TestCapabilitiesCodecsString 库列格式（逗号分隔，无空格）。
func TestCapabilitiesCodecsString(t *testing.T) {
	c := Capabilities{Codecs: []string{"h264", "hevc"}}
	if got := c.CodecsString(); got != "h264,hevc" {
		t.Fatalf("codecs 拼接应为 h264,hevc，实际 %q", got)
	}
	if got := (Capabilities{}).CodecsString(); got != "" {
		t.Fatalf("空 codecs 应拼成空串，实际 %q", got)
	}
}

// TestDefaultNVProberNoPanic 真实探测器在无卡环境下必须安全返回（不 panic、不阻塞）。
func TestDefaultNVProberNoPanic(t *testing.T) {
	p := DefaultNVProber{}
	// 仅要求"能被调用且不 panic"；有卡机器上会返回 true，无卡为 false，两者都算通过。
	_ = p.Available()
	name, vram := p.Info()
	if vram < 0 {
		t.Fatalf("显存不应为负，实际 %d（name=%q）", vram, name)
	}
}
