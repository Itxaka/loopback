# Loopback

Loopback is a Go library for managing Linux loop devices and device-mapper mappings for GPT-partitioned images. It allows you to programmatically attach/detach image files to loop devices, create device-mapper mappings for partitions, and clean up those mappings. This is especially useful for working with disk images, containers, and testing environments.

The idea here is to provide a pure golang implementation that does not rely on external tools like `losetup`, `dmsetup` or `kpartx`, making it easier to integrate into Go applications without additional dependencies or run in environments where those tools are or might not be available.

## Features
- Attach image files to loop devices (using syscalls, no external tools)
- Detach loop devices
- Check if an image is already in use by a loop device
- Create device-mapper mappings for each GPT partition on a loop device
- Clean up device-mapper mappings and device nodes
- Parse GPT partition tables
- Can substitute `losetup` + `kpartx` for managing loop devices and partitions

## Requirements
- libdevmapper development headers (e.g., `libdevmapper-dev` package)
- Sufficient privileges to manage loop devices (require `sudo`)

## Functions

### `Loop(img string, rw bool, log Logger) (string, error)`
Attaches the specified image file to a free loop device and returns the device path (e.g., `/dev/loop0`). The `rw` flag controls read/write access. Requires a `Logger` for logging.

### `Unloop(loopDevice string, log Logger) error`
Detaches the specified loop device and frees the underlying image. Requires a `Logger` for logging.

### `CreateMappingsFromDevice(loopDevice string, log Logger) error`
Creates device-mapper mappings for each GPT partition found on the given loop device. Each partition will appear as a `/dev/mapper/loopXpY` symlink to a `/dev/dm-N` device. Requires a `Logger` for logging.

### `CleanupMappingsForDevice(loopDevice string, log Logger) error`
Removes all device-mapper mappings and device nodes for the given loop device. Requires a `Logger` for logging.

### `GetGPTPartitions(devicePath string) ([]Partition, error)`
Parses the GPT partition table from the given device or image and returns a slice of `Partition` structs with partition info.

## Usage Example

```go
import "github.com/Itxaka/loopback"

var log loopback.Logger = ... // your logger implementation

// Attach image to a loop device
loopDev, err := loopback.Loop("/path/to/image.img", true, log)
defer loopback.Unloop(loopDev, log) // Ensure we detach later

// Create device-mapper mappings for partitions
err = loopback.CreateMappingsFromDevice(loopDev, log)
defer loopback.CleanupMappingsForDevice(loopDev, log)
// ... use /dev/mapper/loopXpY devices ...
```

For more use cases, refer to the source code and tests in the package.

## Notes
- There is currently no CLI provided. This project is intended for use as a Go library.
- You must provide a logger that implements the `Logger` interface (see `log.go`). The standard `log` package can be used, or you can implement your own logger or even use a No-op logger if you don't need logging.

## License
Apache License 2.0. See the LICENSE file for details.
