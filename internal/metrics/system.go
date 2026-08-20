package metrics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
)

// SystemSnapshot is one sampling of every local resource metric. Rate values
// are derived from counter deltas, so the first snapshot reports zero rates.
type SystemSnapshot struct {
	At time.Time

	CPUPercent float64
	CPUDetail  string
	CPUErr     error

	MemPercent float64
	MemDetail  string
	MemErr     error

	DiskPercent float64
	DiskDetail  string
	DiskErr     error

	DiskIORate    float64 // bytes/s, read + write
	DiskReadRate  float64
	DiskWriteRate float64
	DiskIODetail  string
	DiskIOErr     error

	NetRate   float64 // bytes/s, rx + tx
	NetRxRate float64
	NetTxRate float64
	NetDetail string
	NetErr    error

	// Warmup is true while a snapshot still lacks a previous counter sample,
	// which makes its rate values meaningless.
	Warmup bool
}

type ioCounters struct {
	read, write uint64
}

type netCounters struct {
	rx, tx uint64
}

// Collector samples local system metrics, holding the previous counter values
// needed to turn monotonic counters into rates.
type Collector struct {
	cores int

	prevAt  time.Time
	prevIO  ioCounters
	prevNet netCounters
	hasPrev bool
}

func NewCollector() *Collector {
	c := &Collector{}
	if n, err := cpu.Counts(true); err == nil {
		c.cores = n
	}
	// Prime gopsutil's internal CPU snapshot so the first real sample is a
	// delta against process start rather than against nothing.
	_, _ = cpu.Percent(0, false)
	return c
}

// Cores is the number of logical CPUs, which per-service CPU shares are
// measured against.
func (c *Collector) Cores() int {
	if c.cores < 1 {
		return 1
	}
	return c.cores
}

// Collect gathers one snapshot. mount is the filesystem to report usage for and
// iface restricts network counters to a single interface ("" means all).
func (c *Collector) Collect(ctx context.Context, mount, iface string) SystemSnapshot {
	now := time.Now()
	s := SystemSnapshot{At: now, Warmup: !c.hasPrev}

	elapsed := now.Sub(c.prevAt).Seconds()
	if !c.hasPrev || elapsed <= 0 {
		elapsed = 0
	}

	c.collectCPU(ctx, &s)
	c.collectMem(ctx, &s)
	c.collectDisk(ctx, &s, mount)
	c.collectDiskIO(ctx, &s, elapsed)
	c.collectNet(ctx, &s, iface, elapsed)

	c.prevAt = now
	c.hasPrev = true
	return s
}

func (c *Collector) collectCPU(ctx context.Context, s *SystemSnapshot) {
	pct, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		s.CPUErr = err
		return
	}
	if len(pct) > 0 {
		s.CPUPercent = clampPercent(pct[0])
	}
	detail := fmt.Sprintf("%d cores", c.cores)
	if avg, err := load.AvgWithContext(ctx); err == nil {
		detail += fmt.Sprintf(" · load %.2f %.2f %.2f", avg.Load1, avg.Load5, avg.Load15)
	}
	s.CPUDetail = detail
}

func (c *Collector) collectMem(ctx context.Context, s *SystemSnapshot) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		s.MemErr = err
		return
	}
	s.MemPercent = clampPercent(vm.UsedPercent)
	s.MemDetail = fmt.Sprintf("%s / %s", FormatBytes(float64(vm.Used)), FormatBytes(float64(vm.Total)))
}

func (c *Collector) collectDisk(ctx context.Context, s *SystemSnapshot, mount string) {
	if mount == "" {
		mount = "/"
	}
	u, err := disk.UsageWithContext(ctx, mount)
	if err != nil {
		s.DiskErr = err
		return
	}
	s.DiskPercent = clampPercent(u.UsedPercent)
	s.DiskDetail = fmt.Sprintf("%s: %s / %s", mount, FormatBytes(float64(u.Used)), FormatBytes(float64(u.Total)))
}

func (c *Collector) collectDiskIO(ctx context.Context, s *SystemSnapshot, elapsed float64) {
	stats, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		s.DiskIOErr = err
		return
	}
	var cur ioCounters
	for _, st := range stats {
		cur.read += st.ReadBytes
		cur.write += st.WriteBytes
	}
	if elapsed > 0 {
		s.DiskReadRate = rate(cur.read, c.prevIO.read, elapsed)
		s.DiskWriteRate = rate(cur.write, c.prevIO.write, elapsed)
		s.DiskIORate = s.DiskReadRate + s.DiskWriteRate
	}
	c.prevIO = cur
	s.DiskIODetail = fmt.Sprintf("r %s · w %s", FormatRate(s.DiskReadRate), FormatRate(s.DiskWriteRate))
}

func (c *Collector) collectNet(ctx context.Context, s *SystemSnapshot, iface string, elapsed float64) {
	perNIC := iface != ""
	stats, err := psnet.IOCountersWithContext(ctx, perNIC)
	if err != nil {
		s.NetErr = err
		return
	}
	var cur netCounters
	matched := false
	for _, st := range stats {
		if perNIC && !strings.EqualFold(st.Name, iface) {
			continue
		}
		cur.rx += st.BytesRecv
		cur.tx += st.BytesSent
		matched = true
	}
	if perNIC && !matched {
		s.NetErr = fmt.Errorf("interface %q not found", iface)
		return
	}
	if elapsed > 0 {
		s.NetRxRate = rate(cur.rx, c.prevNet.rx, elapsed)
		s.NetTxRate = rate(cur.tx, c.prevNet.tx, elapsed)
		s.NetRate = s.NetRxRate + s.NetTxRate
	}
	c.prevNet = cur
	label := iface
	if label == "" {
		label = "all"
	}
	s.NetDetail = fmt.Sprintf("%s · ↓ %s · ↑ %s", label, FormatRate(s.NetRxRate), FormatRate(s.NetTxRate))
}

// rate turns a counter delta into a per-second value, treating a counter reset
// (device removed, wraparound) as zero rather than a huge spike.
func rate(cur, prev uint64, elapsed float64) float64 {
	if cur < prev || elapsed <= 0 {
		return 0
	}
	return float64(cur-prev) / elapsed
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Mounts lists mountpoints suitable for /disk completion.
func Mounts() []string {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.Mountpoint)
	}
	sort.Strings(out)
	return out
}

// Interfaces lists interface names suitable for /net completion.
func Interfaces() []string {
	stats, err := psnet.IOCounters(true)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(stats))
	for _, s := range stats {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}
