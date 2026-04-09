package android

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/drizz-dev/drizz-farm/internal/config"
)

// MockRunner is a test double for CommandRunner.
type MockRunner struct {
	RunFunc   func(ctx context.Context, name string, args ...string) ([]byte, error)
	StartFunc func(ctx context.Context, name string, args ...string) (*exec.Cmd, error)
	Calls     []MockCall
}

type MockCall struct {
	Name string
	Args []string
}

func (m *MockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.Calls = append(m.Calls, MockCall{Name: name, Args: args})
	if m.RunFunc != nil {
		return m.RunFunc(ctx, name, args...)
	}
	return nil, nil
}

func (m *MockRunner) Start(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	m.Calls = append(m.Calls, MockCall{Name: name, Args: args})
	if m.StartFunc != nil {
		return m.StartFunc(ctx, name, args...)
	}
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// --- Port Allocator Tests ---

func TestPortAllocator(t *testing.T) {
	pa := NewPortAllocator(5554, 5570)

	if pa.Capacity() != 8 {
		t.Errorf("expected capacity 8, got %d", pa.Capacity())
	}

	p1, err := pa.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error: %v", err)
	}
	if p1.Console != 5554 || p1.ADB != 5555 {
		t.Errorf("expected 5554/5555, got %d/%d", p1.Console, p1.ADB)
	}

	p2, err := pa.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error: %v", err)
	}
	if p2.Console != 5556 || p2.ADB != 5557 {
		t.Errorf("expected 5556/5557, got %d/%d", p2.Console, p2.ADB)
	}

	if pa.InUseCount() != 2 {
		t.Errorf("expected 2 in use, got %d", pa.InUseCount())
	}

	pa.Release(p1)
	if pa.InUseCount() != 1 {
		t.Errorf("expected 1 in use after release, got %d", pa.InUseCount())
	}

	// Re-allocate should reuse first pair
	p3, err := pa.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error: %v", err)
	}
	if p3.Console != 5554 {
		t.Errorf("expected reuse of 5554, got %d", p3.Console)
	}
}

func TestPortAllocatorExhaustion(t *testing.T) {
	pa := NewPortAllocator(5554, 5558)

	if _, err := pa.Allocate(); err != nil {
		t.Fatalf("first Allocate() error: %v", err)
	}
	if _, err := pa.Allocate(); err != nil {
		t.Fatalf("second Allocate() error: %v", err)
	}
	if _, err := pa.Allocate(); err == nil {
		t.Fatal("expected error on exhaustion, got nil")
	}
}

func TestPortAllocatorOddMinPort(t *testing.T) {
	pa := NewPortAllocator(5555, 5570)

	p, err := pa.Allocate()
	if err != nil {
		t.Fatalf("Allocate() error: %v", err)
	}
	if p.Console != 5556 {
		t.Errorf("expected 5556 (rounded from odd 5555), got %d", p.Console)
	}
}

// --- ADB Tests ---

func TestADBDevices(t *testing.T) {
	mock := &MockRunner{
		RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("List of devices attached\nemulator-5554\tdevice\nemulator-5556\toffline\n\n"), nil
		},
	}

	sdk := &SDK{Root: "/fake/sdk"}
	adb := NewADBClient(sdk, mock)

	devices, err := adb.Devices(context.Background())
	if err != nil {
		t.Fatalf("Devices() error: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
	if devices[0].Serial != "emulator-5554" || devices[0].State != "device" {
		t.Errorf("unexpected device[0]: %+v", devices[0])
	}
	if devices[1].Serial != "emulator-5556" || devices[1].State != "offline" {
		t.Errorf("unexpected device[1]: %+v", devices[1])
	}
}

func TestADBGetProp(t *testing.T) {
	mock := &MockRunner{
		RunFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			// adb -s emulator-5554 shell getprop sys.boot_completed
			// args: [-s, emulator-5554, shell, getprop, sys.boot_completed]
			if len(args) > 4 && args[4] == "sys.boot_completed" {
				return []byte("1\n"), nil
			}
			return []byte(""), nil
		},
	}

	sdk := &SDK{Root: "/fake/sdk"}
	adb := NewADBClient(sdk, mock)

	val, err := adb.GetProp(context.Background(), "emulator-5554", "sys.boot_completed")
	if err != nil {
		t.Fatalf("GetProp() error: %v", err)
	}
	if val != "1" {
		t.Errorf("expected '1', got '%s'", val)
	}
}

func TestADBListThirdPartyPackages(t *testing.T) {
	mock := &MockRunner{
		RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("package:com.example.app\npackage:com.test.app\n"), nil
		},
	}

	sdk := &SDK{Root: "/fake/sdk"}
	adb := NewADBClient(sdk, mock)

	packages, err := adb.ListThirdPartyPackages(context.Background(), "emulator-5554")
	if err != nil {
		t.Fatalf("ListThirdPartyPackages() error: %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(packages))
	}
	if packages[0] != "com.example.app" {
		t.Errorf("expected 'com.example.app', got '%s'", packages[0])
	}
}

// --- AVD Manager Tests ---

func TestAVDManagerCreate(t *testing.T) {
	mock := &MockRunner{}
	sdk := &SDK{Root: "/fake/sdk"}
	mgr := NewAVDManager(sdk, mock)

	profile := config.AndroidProfile{
		Device:      "pixel_7",
		SystemImage: "system-images;android-34;google_apis;arm64-v8a",
		RAMMB:       2048,
	}

	err := mgr.Create(context.Background(), "test_avd", profile)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}

	argsStr := strings.Join(mock.Calls[0].Args, " ")
	if !strings.Contains(argsStr, "create avd") {
		t.Errorf("expected 'create avd' in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "test_avd") {
		t.Errorf("expected 'test_avd' in args, got: %s", argsStr)
	}
}

func TestAVDManagerList(t *testing.T) {
	mock := &MockRunner{
		RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("drizz_pixel_7_0\ndrizz_pixel_7_1\n"), nil
		},
	}

	sdk := &SDK{Root: "/fake/sdk"}
	mgr := NewAVDManager(sdk, mock)

	avds, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(avds) != 2 {
		t.Fatalf("expected 2 AVDs, got %d", len(avds))
	}
	if avds[0].Name != "drizz_pixel_7_0" {
		t.Errorf("expected 'drizz_pixel_7_0', got '%s'", avds[0].Name)
	}
}

func TestAVDManagerExists(t *testing.T) {
	mock := &MockRunner{
		RunFunc: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("drizz_pixel_7_0\ndrizz_pixel_7_1\n"), nil
		},
	}

	sdk := &SDK{Root: "/fake/sdk"}
	mgr := NewAVDManager(sdk, mock)

	exists, err := mgr.Exists(context.Background(), "drizz_pixel_7_0")
	if err != nil {
		t.Fatalf("Exists() error: %v", err)
	}
	if !exists {
		t.Error("expected drizz_pixel_7_0 to exist")
	}

	exists, err = mgr.Exists(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("Exists() error: %v", err)
	}
	if exists {
		t.Error("expected nonexistent to not exist")
	}
}

func TestAVDName(t *testing.T) {
	name := AVDName("pixel_7_api34", 0)
	if name != "drizz_pixel_7_api34_0" {
		t.Errorf("expected 'drizz_pixel_7_api34_0', got '%s'", name)
	}

	name = AVDName("pixel_7_api34", 3)
	if name != "drizz_pixel_7_api34_3" {
		t.Errorf("expected 'drizz_pixel_7_api34_3', got '%s'", name)
	}
}
