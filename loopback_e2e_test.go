//go:build e2e
// +build e2e

package loopback_test

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/itxaka/loopback"
)

// Set this to the path of a real disk image with GPT partitions for testing
const diskImgPath = "/tmp/disk.img"

func createTestDiskImage(t *testing.T, path string) {
	// Remove if exists
	_ = os.Remove(path)
	// Create a 100MB blank image
	cmd := exec.Command("dd", "if=/dev/zero", "of="+path, "bs=1M", "count=100")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create disk image: %v, output: %s", err, string(out))
	}
	// Create GPT partition table
	cmd = exec.Command("sgdisk", "-o", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create GPT: %v, output: %s", err, string(out))
	}
	// Add a partition (1st partition, 1MB-50MB)
	cmd = exec.Command("sgdisk", "-n", "1:2048:100000", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to add partition: %v, output: %s", err, string(out))
	}
}

func TestEnd2EndLoopback(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	if os.Geteuid() != 0 {
		stdLogger.Printf("Skipping test: must be run as root")
		t.Skip("must be run as root")
	}
	stdLogger.Printf("Starting end-to-end loopback test")
	createTestDiskImage(t, diskImgPath)
	defer func() {
		stdLogger.Printf("Cleaning up: removing disk image %s", diskImgPath)
		os.Remove(diskImgPath)
	}()

	// Attach loop device
	stdLogger.Printf("Attaching loop device to %s", diskImgPath)
	loopDev, err := loopback.Loop(diskImgPath, true, stdLogger)
	if err != nil {
		stdLogger.Fatalf("Loop() failed: %v", err)
	}
	if loopDev == "" {
		stdLogger.Fatalf("Loop() returned empty device")
	}
	stdLogger.Printf("Loop device: %s", loopDev)

	// Discover partitions
	stdLogger.Printf("Discovering GPT partitions on %s", loopDev)
	parts, err := loopback.GetGPTPartitions(loopDev)
	if err != nil {
		stdLogger.Fatalf("GetGPTPartitions() failed: %v", err)
	}
	if len(parts) == 0 {
		stdLogger.Fatalf("No partitions found on %s", loopDev)
	}
	stdLogger.Printf("Partitions: %+v", parts)

	// Create device-mapper mappings
	stdLogger.Printf("Creating device-mapper mappings for %s", loopDev)
	err = loopback.CreateMappingsFromDevice(loopDev, stdLogger)
	if err != nil {
		stdLogger.Fatalf("CreateMappingsFromDevice() failed: %v", err)
	}

	// Wait for /dev/mapper/loopXpY to appear
	for _, p := range parts {
		mapperPath := filepath.Join("/dev/mapper", filepath.Base(loopDev)+"p"+strconv.Itoa(p.Number))
		stdLogger.Printf("Waiting for mapper device %s to appear", mapperPath)
		found := false
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(mapperPath); err == nil {
				found = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !found {
			stdLogger.Printf("mapper device %s not found", mapperPath)
		} else {
			stdLogger.Printf("mapper device %s found", mapperPath)
		}
	}

	// Cleanup device-mapper mappings
	stdLogger.Printf("Cleaning up device-mapper mappings for %s", loopDev)
	err = loopback.CleanupMappingsForDevice(loopDev, stdLogger)
	if err != nil {
		stdLogger.Printf("CleanupMappingsForDevice() failed: %v", err)
	}

	// Detach loop device
	stdLogger.Printf("Detaching loop device %s", loopDev)
	err = loopback.Unloop(loopDev, stdLogger)
	if err != nil {
		stdLogger.Printf("Unloop() failed: %v", err)
	}
	stdLogger.Printf("End-to-end loopback test completed")
}

// Test attaching a non-existent image (should fail)
func TestLoopbackNonExistentImage(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	fakeImg := "/tmp/does_not_exist.img"
	stdLogger.Printf("Testing loopback attach with non-existent image: %s", fakeImg)
	_, err := loopback.Loop(fakeImg, true, stdLogger)
	if err == nil {
		t.Fatalf("Expected error when attaching non-existent image, got nil")
	}
	stdLogger.Printf("Correctly failed to attach non-existent image: %v", err)
}

// Test with multiple partitions
func TestLoopbackMultiplePartitions(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	imgPath := "/tmp/multi_part.img"
	_ = os.Remove(imgPath)
	// Create a 100MB blank image
	cmd := exec.Command("dd", "if=/dev/zero", "of="+imgPath, "bs=1M", "count=100")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create disk image: %v, output: %s", err, string(out))
	}
	// Create GPT partition table
	cmd = exec.Command("sgdisk", "-o", imgPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create GPT: %v, output: %s", err, string(out))
	}
	// Add 3 partitions
	cmd = exec.Command("sgdisk", "-n", "1:2048:100000", "-n", "2:100001:150000", "-n", "3:150001:200000", imgPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to add partitions: %v, output: %s", err, string(out))
	}
	defer os.Remove(imgPath)
	loopDev, err := loopback.Loop(imgPath, true, stdLogger)
	if err != nil {
		t.Fatalf("Loop() failed: %v", err)
	}
	parts, err := loopback.GetGPTPartitions(loopDev)
	if err != nil {
		t.Fatalf("GetGPTPartitions() failed: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("Expected 3 partitions, got %d", len(parts))
	}
	stdLogger.Printf("Found 3 partitions as expected: %+v", parts)
	err = loopback.CleanupMappingsForDevice(loopDev, stdLogger)
	_ = loopback.Unloop(loopDev, stdLogger)
}

// Test cleanup robustness (double cleanup)
func TestLoopbackDoubleCleanup(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	imgPath := "/tmp/double_cleanup.img"
	createTestDiskImage(t, imgPath)
	defer os.Remove(imgPath)
	loopDev, err := loopback.Loop(imgPath, true, stdLogger)
	if err != nil {
		t.Fatalf("Loop() failed: %v", err)
	}
	err = loopback.CleanupMappingsForDevice(loopDev, stdLogger)
	if err != nil {
		stdLogger.Printf("First cleanup error: %v", err)
	}
	err = loopback.CleanupMappingsForDevice(loopDev, stdLogger)
	if err != nil {
		stdLogger.Printf("Second cleanup error (should be safe): %v", err)
	}
	_ = loopback.Unloop(loopDev, stdLogger)
}

// Test idempotency (double unloop)
func TestLoopbackDoubleUnloop(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	imgPath := "/tmp/double_unloop.img"
	createTestDiskImage(t, imgPath)
	defer os.Remove(imgPath)
	loopDev, err := loopback.Loop(imgPath, true, stdLogger)
	if err != nil {
		t.Fatalf("Loop() failed: %v", err)
	}
	_ = loopback.Unloop(loopDev, stdLogger)
	err = loopback.Unloop(loopDev, stdLogger)
	if err != nil {
		stdLogger.Printf("Second unloop error (should be safe): %v", err)
	}
}

// Test corrupted image (random data, not a valid disk)
func TestLoopbackCorruptedImage(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	imgPath := "/tmp/corrupted.img"
	_ = os.Remove(imgPath)
	// Create a 1MB file with random data
	cmd := exec.Command("dd", "if=/dev/urandom", "of="+imgPath, "bs=1M", "count=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create corrupted image: %v, output: %s", err, string(out))
	}
	defer os.Remove(imgPath)
	loopDev, err := loopback.Loop(imgPath, true, stdLogger)
	if err != nil {
		stdLogger.Printf("Loop() failed as expected for corrupted image: %v", err)
		return
	}
	_, err = loopback.GetGPTPartitions(loopDev)
	if err == nil {
		t.Fatalf("Expected error when reading partitions from corrupted image, got nil")
	}
	_ = loopback.Unloop(loopDev, stdLogger)
	stdLogger.Printf("Correctly failed to read partitions from corrupted image: %v", err)
}

// Test read-only mode
func TestLoopbackReadOnly(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	imgPath := "/tmp/readonly.img"
	createTestDiskImage(t, imgPath)
	defer os.Remove(imgPath)
	loopDev, err := loopback.Loop(imgPath, false, stdLogger) // false = read-only
	if err != nil {
		t.Fatalf("Loop() failed in read-only mode: %v", err)
	}
	parts, err := loopback.GetGPTPartitions(loopDev)
	if err != nil {
		t.Fatalf("GetGPTPartitions() failed in read-only mode: %v", err)
	}
	if len(parts) == 0 {
		t.Fatalf("No partitions found in read-only mode")
	}
	err = loopback.CleanupMappingsForDevice(loopDev, stdLogger)
	_ = loopback.Unloop(loopDev, stdLogger)
	stdLogger.Printf("Read-only mode test completed successfully")
}

// Test missing partition table (blank image)
func TestLoopbackBlankImage(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	imgPath := "/tmp/blank.img"
	_ = os.Remove(imgPath)
	cmd := exec.Command("dd", "if=/dev/zero", "of="+imgPath, "bs=1M", "count=10")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create blank image: %v, output: %s", err, string(out))
	}
	defer os.Remove(imgPath)
	loopDev, err := loopback.Loop(imgPath, true, stdLogger)
	if err != nil {
		stdLogger.Printf("Loop() failed as expected for blank image: %v", err)
		return
	}
	_, err = loopback.GetGPTPartitions(loopDev)
	if err == nil {
		t.Fatalf("Expected error when reading partitions from blank image, got nil")
	}
	_ = loopback.Unloop(loopDev, stdLogger)
	stdLogger.Printf("Correctly failed to read partitions from blank image: %v", err)
}

// Test attaching an image already in use
func TestLoopbackAttachTwice(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	imgPath := "/tmp/twice.img"
	createTestDiskImage(t, imgPath)
	defer os.Remove(imgPath)
	loopDev1, err := loopback.Loop(imgPath, true, stdLogger)
	if err != nil {
		t.Fatalf("First Loop() failed: %v", err)
	}
	loopDev2, err := loopback.Loop(imgPath, true, stdLogger)
	if err == nil {
		_ = loopback.Unloop(loopDev2, stdLogger)
		t.Fatalf("Expected error when attaching image already in use, got nil")
	}
	_ = loopback.Unloop(loopDev1, stdLogger)
	stdLogger.Printf("Correctly failed to attach image already in use: %v", err)
}

// Test cleanup after partial failure (simulate by calling cleanup on random device)
func TestLoopbackCleanupPartialFailure(t *testing.T) {
	stdLogger := log.New(os.Stdout, "[loopback test] ", log.LstdFlags)
	fakeDev := "/dev/loop9999"
	err := loopback.CleanupMappingsForDevice(fakeDev, stdLogger)
	if err != nil {
		stdLogger.Printf("CleanupMappingsForDevice() failed as expected for fake device: %v", err)
	} else {
		stdLogger.Printf("CleanupMappingsForDevice() did not fail for fake device (unexpected)")
	}
}
