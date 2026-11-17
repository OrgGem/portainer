//go:build linux

package resourcemetrics

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func readHost(root, name string) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 4<<20))
}

// OpenRoot confines symlink resolution to the mounted host root. Never stat a
// daemon-provided path in Portainer's own filesystem namespace.
func HostFilesystem(root, source string) *Metric {
	if !strings.HasPrefix(source, "/") || strings.Contains(source, "\x00") {
		return nil
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil
	}
	defer r.Close()
	f, err := r.Open(strings.TrimPrefix(source, "/"))
	if err != nil && source == "/" {
		f, err = r.Open(".")
	}
	if err != nil {
		return nil
	}
	defer f.Close()
	var s unix.Statfs_t
	if unix.Fstatfs(int(f.Fd()), &s) != nil || s.Blocks == 0 {
		return nil
	}
	m := Metric{Used: Number(float64(s.Blocks-s.Bfree) * float64(s.Bsize)), Total: Number(float64(s.Blocks) * float64(s.Bsize)), Unit: "bytes", Basis: "shared host filesystem; not a volume quota"}
	return &m
}

func cpuTicks(b []byte) (total, idle uint64, err error) {
	line := strings.SplitN(string(b), "\n", 2)[0]
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, errors.New("missing CPU counters")
	}
	// Guest counters are already included in user/nice; don't count them twice.
	for i := 1; i < len(f) && i <= 8; i++ {
		n, e := strconv.ParseUint(f[i], 10, 64)
		if e != nil {
			return 0, 0, e
		}
		total += n
		if i == 4 || i == 5 {
			idle += n
		}
	}
	return total, idle, nil
}

func Host(ctx context.Context, root string, info Info) Usage {
	u := Usage{SampledAt: time.Now().UTC(), CPU: Unknown("cores", "host CPU capacity"), Memory: Unknown("bytes", "host physical memory"), State: "host", Source: "Linux host (Docker VM when using Docker Desktop)", Mounts: []Mount{}, Notes: []string{}}
	if info.NCPU > 0 {
		u.CPU.Total = Number(float64(info.NCPU))
	}
	if info.MemTotal > 0 {
		u.Memory.Total = Number(float64(info.MemTotal))
	}
	first, e1 := readHost(root, "proc/stat")
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		u.Notes = append(u.Notes, "Host sample cancelled")
		return u
	case <-timer.C:
	}
	second, e2 := readHost(root, "proc/stat")
	a, ai, ea := cpuTicks(first)
	b, bi, eb := cpuTicks(second)
	if e1 == nil && e2 == nil && ea == nil && eb == nil && b > a && bi >= ai && b-a >= bi-ai && info.NCPU > 0 {
		u.CPU.Used = Number(float64((b-a)-(bi-ai)) / float64(b-a) * float64(info.NCPU))
	} else {
		u.Notes = append(u.Notes, "Host CPU counters unavailable")
	}
	mem, err := readHost(root, "proc/meminfo")
	if err == nil {
		values := map[string]float64{}
		for _, line := range strings.Split(string(mem), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				n, e := strconv.ParseFloat(f[1], 64)
				if e == nil {
					values[strings.TrimSuffix(f[0], ":")] = n * 1024
				}
			}
		}
		total, ok := values["MemTotal"]
		available, hasAvailable := values["MemAvailable"]
		if ok && hasAvailable && total > 0 && available <= total {
			u.Memory.Total = Number(total)
			u.Memory.Used = Number(total - available)
		}
	}
	if u.Memory.Used == nil {
		u.Notes = append(u.Notes, "Host memory counters unavailable")
	}
	data, err := readHost(root, "proc/1/mountinfo")
	if err == nil {
		u.Mounts = hostMounts(root, string(data))
		prioritizeMounts(u.Mounts, info.DockerRootDir)
	}
	if len(u.Mounts) == 0 {
		u.Notes = append(u.Notes, "Host mount capacity unavailable; enable the read-only host metrics mount")
	}
	u.SampledAt = time.Now().UTC()
	u.StorageSampledAt = &u.SampledAt
	return u
}

// Show the filesystem backing Docker before boot partitions and other mounts.
func prioritizeMounts(mounts []Mount, dockerRoot string) {
	best := ""
	for _, m := range mounts {
		if (dockerRoot == m.Target || strings.HasPrefix(dockerRoot, strings.TrimSuffix(m.Target, "/")+"/")) && len(m.Target) > len(best) {
			best = m.Target
		}
	}
	sort.SliceStable(mounts, func(i, j int) bool {
		priority := func(p string) int {
			if p == best {
				return 0
			}
			if p == "/" {
				return 1
			}
			return 2
		}
		a, b := priority(mounts[i].Target), priority(mounts[j].Target)
		if a != b {
			return a < b
		}
		return mounts[i].Target < mounts[j].Target
	})
}

func hostMounts(root, data string) []Mount {
	mounts := []Mount{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	unescape := strings.NewReplacer("\\040", " ", "\\011", "\t", "\\012", "\n", "\\134", "\\")
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) < 6 || len(right) < 2 {
			continue
		}
		target, fs := unescape.Replace(left[4]), right[0]
		if target != "/" {
			switch fs {
			case "proc", "sysfs", "tmpfs", "devtmpfs", "devpts", "cgroup", "cgroup2", "overlay", "squashfs", "nsfs", "mqueue", "securityfs", "debugfs", "tracefs", "pstore", "bpf", "autofs", "fusectl", "configfs", "hugetlbfs":
				continue
			}
			if strings.HasPrefix(target, "/proc/") || strings.HasPrefix(target, "/sys/") || strings.HasPrefix(target, "/run/") || strings.Contains(target, "/docker/containers/") || strings.Contains(target, "/docker/overlay") || strings.Contains(target, "/containerd/") {
				continue
			}
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		if capacity := HostFilesystem(root, target); capacity != nil {
			mounts = append(mounts, Mount{Target: target, Name: unescape.Replace(right[1]), Type: fs, ReadOnly: strings.Contains(","+left[5]+",", ",ro,"), Data: *capacity})
		}
		if len(mounts) >= 128 {
			break
		}
	}
	return mounts
}
