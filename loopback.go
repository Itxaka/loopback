package loopback

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// syscalls will return an errno type (which implements error) for all calls,
// including success (errno 0) so we need to check its value to know if its an actual error or not
func errnoIsErr(err error) error {
	if err != nil && err.(syscall.Errno) != 0 {
		return err
	}

	return nil
}

// isImageInUse checks if the given image file is already attached to a loop device
func isImageInUse(imagePath string) (bool, error) {
	// Get absolute path to properly compare with the backing files
	absImagePath, err := filepath.Abs(imagePath)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check /sys/block/loop* directories to find backing files
	loopDirs, err := filepath.Glob("/sys/block/loop*")
	if err != nil {
		return false, fmt.Errorf("failed to list loop devices: %w", err)
	}

	for _, loopDir := range loopDirs {
		backingFilePath := filepath.Join(loopDir, "loop", "backing_file")

		// Read the backing file path if it exists
		backingFile, err := os.ReadFile(backingFilePath)
		if err == nil {
			// Trim null bytes and newlines
			backingFileTrimmed := strings.TrimSpace(strings.TrimRight(string(backingFile), "\x00"))

			// Compare with our image path
			if backingFileTrimmed == absImagePath {
				return true, nil
			}
		}
	}

	return false, nil
}

// Loop will set up a /dev/loopX device linked to the image file by using syscalls directly to set it
func Loop(img string, rw bool, log Logger) (loopDevice string, err error) {
	// Check if image is already in use
	inUse, err := isImageInUse(img)
	if err != nil {
		log.Printf("Warning: Failed to check if image is in use: %v", err)
	} else if inUse {
		return "", fmt.Errorf("image file %s is already in use by another loop device", img)
	}

	log.Printf("Opening loop control device")
	fd, err := os.OpenFile("/dev/loop-control", os.O_RDONLY, 0o644)
	if err != nil {
		log.Printf("failed to open /dev/loop-control")
		return loopDevice, err
	}

	defer fd.Close()
	log.Printf("Getting free loop device")
	loopInt, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), unix.LOOP_CTL_GET_FREE, 0)
	if errnoIsErr(err) != nil {
		log.Printf("failed to get loop device")
		return loopDevice, err
	}

	loopDevice = fmt.Sprintf("/dev/loop%d", loopInt)
	log.Printf("Opening loop device %s", loopDevice)
	loopFile, err := os.OpenFile(loopDevice, os.O_RDWR, 0)
	if err != nil {
		log.Printf("failed to open loop device")
		return loopDevice, err
	}
	log.Printf("Opening image file %s", img)
	imageFile, err := os.OpenFile(img, os.O_RDWR, os.ModePerm)
	if err != nil {
		log.Printf("failed to open image file")
		return loopDevice, err
	}
	defer loopFile.Close()
	defer imageFile.Close()

	log.Printf("Setting loop device")
	_, _, err = syscall.Syscall(
		syscall.SYS_IOCTL,
		loopFile.Fd(),
		unix.LOOP_SET_FD,
		imageFile.Fd(),
	)
	if errnoIsErr(err) != nil {
		log.Printf("failed to set loop device")
		return loopDevice, err
	}

	status := &unix.LoopInfo64{}
	// Dont set read only flag
	if !rw {
		status.Flags &= ^uint32(unix.LO_FLAGS_READ_ONLY)
	}

	log.Printf("Setting loop flags")
	_, _, err = syscall.Syscall(
		syscall.SYS_IOCTL,
		loopFile.Fd(),
		unix.LOOP_SET_STATUS64,
		uintptr(unsafe.Pointer(status)),
	)

	if errnoIsErr(err) != nil {
		log.Printf("failed to set loop device status")
		return loopDevice, err
	}

	return loopDevice, nil
}

// Unloop will clear a loop device and free the underlying image linked to it
func Unloop(loopDevice string, log Logger) error {
	log.Printf("Clearing loop device %s", loopDevice)
	fd, err := os.OpenFile(loopDevice, os.O_RDONLY, 0o644)
	if err != nil {
		log.Printf("failed to set open loop device")
		return err
	}
	defer fd.Close()
	log.Printf("Clearing loop device")
	_, _, err = syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), unix.LOOP_CLR_FD, 0)

	if errnoIsErr(err) != nil {
		log.Printf("failed to set loop device status")
		return err
	}

	return nil
}
