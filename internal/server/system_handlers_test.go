package server

import "testing"

func TestParseNvidiaXML(t *testing.T) {
	input := []byte(`<?xml version="1.0" ?>
<nvidia_smi_log>
  <driver_version>550.54.14</driver_version>
  <cuda_version>12.4</cuda_version>
  <gpu>
    <product_name>NVIDIA RTX 4090</product_name>
    <uuid>GPU-abc</uuid>
    <fb_memory_usage>
      <total>24564 MiB</total>
      <used>1024 MiB</used>
    </fb_memory_usage>
    <utilization>
      <gpu_util>75 %</gpu_util>
      <memory_util>22 %</memory_util>
    </utilization>
    <temperature>
      <gpu_temp>65 C</gpu_temp>
    </temperature>
    <gpu_power_readings>
      <power_draw>320.50 W</power_draw>
      <power_limit>450.00 W</power_limit>
    </gpu_power_readings>
  </gpu>
</nvidia_smi_log>`)

	gpus, err := parseNvidiaXML(input)
	if err != nil {
		t.Fatalf("parseNvidiaXML() error = %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("parseNvidiaXML() returned %d GPUs, want 1", len(gpus))
	}
	gpu := gpus[0]
	if gpu.Index != 0 || gpu.Name != "NVIDIA RTX 4090" || gpu.UUID != "GPU-abc" {
		t.Fatalf("parseNvidiaXML() identity = %#v", gpu)
	}
	assertIntPtr(t, "utilization gpu", gpu.UtilizationGPU, 75)
	assertIntPtr(t, "utilization memory", gpu.UtilizationMemory, 22)
	assertIntPtr(t, "memory total", gpu.MemoryTotalMiB, 24564)
	assertIntPtr(t, "memory used", gpu.MemoryUsedMiB, 1024)
	assertIntPtr(t, "temperature", gpu.TemperatureC, 65)
	assertFloatPtr(t, "power draw", gpu.PowerDrawW, 320.50)
	assertFloatPtr(t, "power limit", gpu.PowerLimitW, 450.00)
	if gpu.DriverVersion != "550.54.14" || gpu.CUDAVersion != "12.4" {
		t.Fatalf("parseNvidiaXML() versions = %q/%q", gpu.DriverVersion, gpu.CUDAVersion)
	}
}

func TestParseNvidiaCSV(t *testing.T) {
	input := []byte("1, NVIDIA RTX 6000 Ada, GPU-def, 10 %, 3 %, 49140 MiB, 2048 MiB, 44, 88.25 W, 300 W\n")

	gpus, err := parseNvidiaCSV(input)
	if err != nil {
		t.Fatalf("parseNvidiaCSV() error = %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("parseNvidiaCSV() returned %d GPUs, want 1", len(gpus))
	}
	gpu := gpus[0]
	if gpu.Index != 1 || gpu.Name != "NVIDIA RTX 6000 Ada" || gpu.UUID != "GPU-def" {
		t.Fatalf("parseNvidiaCSV() identity = %#v", gpu)
	}
	assertIntPtr(t, "utilization gpu", gpu.UtilizationGPU, 10)
	assertIntPtr(t, "utilization memory", gpu.UtilizationMemory, 3)
	assertIntPtr(t, "memory total", gpu.MemoryTotalMiB, 49140)
	assertIntPtr(t, "memory used", gpu.MemoryUsedMiB, 2048)
	assertIntPtr(t, "temperature", gpu.TemperatureC, 44)
	assertFloatPtr(t, "power draw", gpu.PowerDrawW, 88.25)
	assertFloatPtr(t, "power limit", gpu.PowerLimitW, 300)
}

func TestParseNvidiaCSVRejectsEmptyOrShortRows(t *testing.T) {
	for _, input := range [][]byte{nil, []byte("0, only, three\n")} {
		if gpus, err := parseNvidiaCSV(input); err == nil {
			t.Fatalf("parseNvidiaCSV(%q) = %#v, want error", input, gpus)
		}
	}
}

func assertIntPtr(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", name, got, want)
	}
}

func assertFloatPtr(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %g", name, got, want)
	}
}
