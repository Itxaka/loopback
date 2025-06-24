package loopback

/*
#cgo LDFLAGS: -ldevmapper
#include <libdevmapper.h>

enum {
	DeviceCreate = 0,
	DeviceResume = 5,
};

#define ADD_NODE_ON_RESUME DM_ADD_NODE_ON_RESUME
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// CreateMappingsFromDevice sets up device-mapper mappings for each GPT partition on the specified loop device.
func CreateMappingsFromDevice(loopDevice string, log Logger) error {
	log.Printf("Starting device-mapper setup for %s", loopDevice)

	partitions, err := GetGPTPartitions(loopDevice)
	if err != nil {
		return fmt.Errorf("failed to read GPT partitions from %s: %w", loopDevice, err)
	}

	for _, p := range partitions {
		dmName := fmt.Sprintf("loop%dp%d", getLoopNumber(loopDevice), p.Number)
		log.Printf("Creating mapping for partition %d (%s)", p.Number, dmName)

		taskCreate := C.dm_task_create(C.int(C.DeviceCreate))
		if taskCreate == nil {
			return fmt.Errorf("dm_task_create for DeviceCreate failed for %s", dmName)
		}
		defer C.dm_task_destroy(taskCreate)

		dmNameC := C.CString(dmName)
		defer C.free(unsafe.Pointer(dmNameC))

		if C.dm_task_set_name(taskCreate, dmNameC) != 1 {
			return fmt.Errorf("dm_task_set_name failed for %s", dmName)
		}

		targetType := C.CString("linear")
		targetParams := C.CString(fmt.Sprintf("%s %d", loopDevice, p.FirstLBA))
		defer C.free(unsafe.Pointer(targetType))
		defer C.free(unsafe.Pointer(targetParams))

		if C.dm_task_add_target(taskCreate, 0, C.uint64_t(p.NumSectors), targetType, targetParams) != 1 {
			return fmt.Errorf("dm_task_add_target failed for %s", dmName)
		}

		if C.dm_task_set_add_node(taskCreate, C.ADD_NODE_ON_RESUME) != 1 {
			return fmt.Errorf("dm_task_set_add_node failed for %s", dmName)
		}

		if C.dm_task_run(taskCreate) != 1 {
			return fmt.Errorf("dm_task_run (DeviceCreate) failed for %s", dmName)
		}

		log.Printf("Device %s created (suspended state)", dmName)

		taskResume := C.dm_task_create(C.int(C.DeviceResume))
		if taskResume == nil {
			return fmt.Errorf("dm_task_create for DeviceResume failed for %s", dmName)
		}
		defer C.dm_task_destroy(taskResume)

		if C.dm_task_set_name(taskResume, dmNameC) != 1 {
			return fmt.Errorf("dm_task_set_name (resume) failed for %s", dmName)
		}

		if C.dm_task_run(taskResume) != 1 {
			return fmt.Errorf("dm_task_run (DeviceResume) failed for %s", dmName)
		}

		log.Printf("Device %s resumed (active)", dmName)

		// Trigger udev or manually create device node if necessary
		if C.dm_udev_wait(C.uint32_t(0)) != 1 {
			log.Printf("dm_udev_wait failed for %s, node may not be created automatically", dmName)
		}

		// After resuming the device, manually create a device node under /dev/dm-NUMBER
		dmNum := getLoopNumber(dmName) // Use the partition number as the dm number
		dmDevPath := fmt.Sprintf("/dev/dm-%d", dmNum)
		dmPath := "/dev/mapper/" + dmName
		if stat, err := os.Stat(dmPath); err == nil {
			rdev := stat.Sys().(*syscall.Stat_t).Rdev
			major := unix.Major(rdev)
			minor := unix.Minor(rdev)
			// Remove if already exists
			os.Remove(dmDevPath)
			// Create the device node under /dev/dm-NUMBER
			err = unix.Mknod(dmDevPath, unix.S_IFBLK|0600, int(unix.Mkdev(major, minor)))
			if err != nil {
				log.Printf("Failed to create device node %s: %v", dmDevPath, err)
			} else {
				log.Printf("Created device node %s (major:minor = %d:%d)", dmDevPath, major, minor)
				// Remove symlink if it exists
				os.Remove(dmPath)
				// Create the symlink from /dev/mapper/loopXpY to ../dm-NUMBER (relative)
				relTarget, relErr := filepath.Rel(filepath.Dir(dmPath), dmDevPath)
				if relErr != nil {
					relTarget = dmDevPath // fallback to absolute if relative fails
				}
				err = os.Symlink(relTarget, dmPath)
				if err != nil {
					log.Printf("Failed to create symlink %s -> %s: %v", dmPath, relTarget, err)
				} else {
					log.Printf("Created symlink %s -> %s", dmPath, relTarget)
				}
			}
			log.Printf("Device %s ready (major:minor = %d:%d)", dmPath, major, minor)
		} else {
			log.Printf("Device node %s not found: %v", dmPath, err)
		}
	}
	return nil
}

// CleanupMappingsForDevice removes device-mapper mappings and device nodes for a given loop device.
func CleanupMappingsForDevice(loopDevice string, log Logger) error {
	loopNum := getLoopNumber(loopDevice)
	pattern := fmt.Sprintf("loop%dp", loopNum) // e.g. loop0p
	mapperDir := "/dev/mapper"
	entries, err := os.ReadDir(mapperDir)
	if err != nil {
		log.Printf("Failed to read %s: %v", mapperDir, err)
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, pattern) {
			mapperPath := filepath.Join(mapperDir, name)
			// Remove symlink
			if err := os.Remove(mapperPath); err != nil && !os.IsNotExist(err) {
				log.Printf("Failed to remove symlink %s: %v", mapperPath, err)
			}
			// Remove /dev/dm-N device node
			partNum := getPartitionNumber(name)
			if partNum > 0 {
				dmDevPath := fmt.Sprintf("/dev/dm-%d", partNum)
				if err := os.Remove(dmDevPath); err != nil && !os.IsNotExist(err) {
					log.Printf("Failed to remove device node %s: %v", dmDevPath, err)
				}
			}
			// Remove device-mapper mapping using libdevmapper C API
			dmNameC := C.CString(name)
			taskRemove := C.dm_task_create(C.int(2)) // 2 = DeviceRemove
			if taskRemove == nil {
				log.Printf("dm_task_create for DeviceRemove failed for %s", name)
			} else {
				defer C.dm_task_destroy(taskRemove)
				if C.dm_task_set_name(taskRemove, dmNameC) != 1 {
					log.Printf("dm_task_set_name failed for %s", name)
				} else if C.dm_task_run(taskRemove) != 1 {
					log.Printf("dm_task_run (DeviceRemove) failed for %s", name)
				} else {
					log.Printf("Removed mapping %s", name)
				}
				C.free(unsafe.Pointer(dmNameC))
			}
		}
	}
	return nil
}

// getLoopNumber extracts the loop device number from its path
func getLoopNumber(device string) int {
	base := filepath.Base(device) // "loop0"
	numStr := strings.TrimPrefix(base, "loop")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0 // Default to 0 or handle error appropriately
	}
	return num
}

// getPartitionNumber extracts the partition number from a name like loop0p1
func getPartitionNumber(name string) int {
	idx := strings.LastIndex(name, "p")
	if idx == -1 || idx+1 >= len(name) {
		return 0
	}
	partStr := name[idx+1:]
	partNum, err := strconv.Atoi(partStr)
	if err != nil {
		return 0
	}
	return partNum
}
