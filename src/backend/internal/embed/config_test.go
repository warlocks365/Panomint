package embed

// 配置层单测（纯逻辑，无 CGO 依赖，双模式均可运行）。
// 重点覆盖 GPU / CPU 双接口的配置解析——接口必须始终存在，
// 不因开发环境不具备 GPU 而缺席。

import (
	"os"
	"testing"
)

func TestParseDevice(t *testing.T) {
	cases := map[string]DeviceKind{
		"cpu":   DeviceCPU,
		"CPU":   DeviceCPU,
		" cuda ": DeviceCUDA,
		"cuda":  DeviceCUDA,
		"gpu":   DeviceCUDA,
		"auto":  DeviceAuto,
		"":      DeviceAuto,
		"weird": DeviceAuto,
	}
	for in, want := range cases {
		if got := ParseDevice(in); got != want {
			t.Errorf("ParseDevice(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("EMBED_DEVICE", "cuda")
	t.Setenv("EMBED_DEVICE_ID", "2")
	t.Setenv("EMBED_THREADS", "4")
	t.Setenv("EMBED_GPU_MEM_MB", "2048")
	t.Setenv("EMBED_LIB", "/opt/ort/libonnxruntime.so")
	t.Setenv("EMBED_MODEL_DIR", "/models/clip")

	c := ConfigFromEnv()
	if c.Device != DeviceCUDA {
		t.Errorf("Device = %q，期望 cuda", c.Device)
	}
	if c.DeviceID != 2 {
		t.Errorf("DeviceID = %d，期望 2", c.DeviceID)
	}
	if c.IntraThreads != 4 {
		t.Errorf("IntraThreads = %d，期望 4", c.IntraThreads)
	}
	if c.GpuMemLimitMB != 2048 {
		t.Errorf("GpuMemLimitMB = %d，期望 2048", c.GpuMemLimitMB)
	}
	if c.LibPath != "/opt/ort/libonnxruntime.so" {
		t.Errorf("LibPath = %q", c.LibPath)
	}
	if c.ModelDir != "/models/clip" {
		t.Errorf("ModelDir = %q", c.ModelDir)
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	for _, k := range []string{"EMBED_DEVICE", "EMBED_DEVICE_ID", "EMBED_THREADS", "EMBED_GPU_MEM_MB", "EMBED_LIB", "EMBED_MODEL_DIR"} {
		os.Unsetenv(k)
	}
	c := ConfigFromEnv()
	if c.Device != DeviceAuto {
		t.Errorf("未设置时 Device 应为 auto，实得 %q", c.Device)
	}
	if c.DeviceID != 0 || c.IntraThreads != 0 || c.GpuMemLimitMB != 0 {
		t.Errorf("未设置时数值项应为 0：%+v", c)
	}
}

// TestDeviceInterfaceAlwaysPresent 断言双接口在任意构建下都存在（编译期 + 运行期）。
func TestDeviceInterfaceAlwaysPresent(t *testing.T) {
	// 三个设备常量必须齐备——接口保留不随构建标签裁剪
	for _, d := range []DeviceKind{DeviceCPU, DeviceCUDA, DeviceAuto} {
		if d == "" {
			t.Fatal("设备常量不应为空")
		}
	}
	// 占位/真实实现都应提供 Device()/Provider()/LibPath() 访问器
	var e *Encoder
	if e != nil {
		_ = e.Device()
	}
}
