// Package crashes is the kernel crashes cameras recovered from: the bundle
// the firmware's S98crashlog makes of pstore's records after a panic or an
// oops (/etc/crash/crash.tar.gz), sent by the WebUI or downloaded from it
// and sent from /club. A crash is filed under its signature -- the top of
// its backtrace, without offsets -- so one bug sent by a hundred cameras is
// one row, ranked by how bad it is and how many cameras it hits.
//
// The logs are the maintainers' (redacted); the signatures are public.
// Stars are paid for them by the nightly settlement, never by an upload.
package crashes

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Limits on one bundle.
const (
	MaxBundle   = 256 << 10 // compressed, as sent
	MaxUnpacked = 1 << 20   // every record together
	maxRecords  = 16
	sigFrames   = 5 // frames a signature is made of
)

// Kinds, worst first.
const (
	KindBootloop = "bootloop" // the watchdog's bootlimit ran out: failsafe, no record
	KindPanic    = "panic"
	KindOops     = "oops"
	KindBug      = "bug"
	KindWarning  = "warning"
)

var kindRank = map[string]int{KindBootloop: 5, KindPanic: 4, KindOops: 3, KindBug: 2, KindWarning: 1}

// Worse says whether kind a is worse than b.
func Worse(a, b string) bool { return kindRank[a] > kindRank[b] }

// Frame is one function in a backtrace.
type Frame struct {
	Fn     string `json:"fn"`
	Module string `json:"module,omitempty"`
}

func (f Frame) String() string {
	if f.Module == "" {
		return f.Fn
	}
	return f.Fn + " [" + f.Module + "]"
}

// Trace is one report the kernel printed: an oops, a BUG, a WARNING, a panic.
type Trace struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"` // "NULL pointer dereference", "WARNING at mm/page_alloc.c:7373", ...
	Comm   string `json:"comm,omitempty"`
	PC     string `json:"pc,omitempty"`
	InIRQ  bool   `json:"in_irq,omitempty"`
	// Frames: the backtrace, innermost first, without the frames every
	// crash has (dump_stack, the exception and IRQ entry, the syscall).
	Frames []Frame `json:"frames"`
	// Interrupted: what the CPU was doing when the interrupt that crashed came.
	Interrupted []Frame `json:"interrupted,omitempty"`
	end         string  // the "end trace" id, to tell the same trace in two records
}

// Signature is the trace's identity: the same bug on any build gives the same.
func (t *Trace) Signature() string {
	class := "fatal"
	if t.Kind == KindWarning {
		class = "warning"
	}
	var b strings.Builder
	b.WriteString(class)
	for i, f := range t.Frames {
		if i == sigFrames {
			break
		}
		b.WriteString("\n" + f.String())
	}
	if len(t.Frames) == 0 {
		b.WriteString("\n" + t.Reason)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// Title names the trace for a person: why, and where in whose code.
func (t *Trace) Title() string {
	if len(t.Frames) == 0 {
		return t.Reason
	}
	where := t.Frames[0].Fn
	// The first frame in a vendor module says more than a kernel helper,
	// unless it is the module everything calls through.
	for _, f := range t.Frames[:min(len(t.Frames), sigFrames)] {
		if f.Module != "" && !wrapperModule(f.Module) {
			if f.Fn != where {
				where += " ← " + f.String()
			}
			break
		}
	}
	return t.Reason + " in " + where
}

func wrapperModule(m string) bool {
	return strings.HasSuffix(m, "osal") || strings.HasSuffix(m, "_base")
}

// Crash is what one bundle says: one boot that ended in a crash.
type Crash struct {
	Kind  string `json:"kind"`
	Fatal *Trace `json:"fatal"`
	// Before: the warnings the kernel printed on its way there.
	Before []*Trace `json:"before,omitempty"`
	// SelfInflicted: the crash was asked for (sysrq-trigger), not suffered.
	SelfInflicted bool `json:"self_inflicted,omitempty"`

	Kernel      string    `json:"kernel,omitempty"`       // 4.9.37
	KernelBuild string    `json:"kernel_build,omitempty"` // #1 Thu Sep 24 17:57:19 UTC 2026
	KernelBuilt time.Time `json:"kernel_built,omitempty"`
	Toolchain   string    `json:"toolchain,omitempty"`
	Machine     string    `json:"machine,omitempty"`
	Cmdline     string    `json:"cmdline,omitempty"`
	SoC         string    `json:"soc,omitempty"`
	Sensor      string    `json:"sensor,omitempty"`
	Board       string    `json:"board,omitempty"`
	Modules     []string  `json:"modules,omitempty"`
	// Anomalies: lines that often come before a crash, counted.
	Anomalies map[string]int `json:"anomalies,omitempty"`
	// Leadup: the last lines before the fatal trace.
	Leadup []string `json:"leadup,omitempty"`
	// Uptime: seconds from boot to the crash, when printk prints the time.
	Uptime float64 `json:"uptime,omitempty"`
	// Records: how many pstore records the bundle had; Failsafe the
	// firmware's note that the bootlimit ran out.
	Records  int  `json:"records"`
	Failsafe bool `json:"failsafe,omitempty"`

	// Text is every record, as sent; ContentSum identifies the crash
	// whatever tar or gzip carried it.
	Text       string `json:"-"`
	ContentSum string `json:"-"`
}

// ErrNotACrash is a bundle with nothing a crash leaves.
var ErrNotACrash = errors.New("the bundle has no pstore record (dmesg-*) and no failsafe note: nothing crashed")

// Unpack reads a crash bundle -- a tar.gz, a plain tar, or a single record
// as text -- into its records by name.
func Unpack(data []byte) (map[string]string, error) {
	if len(data) > MaxBundle {
		return nil, fmt.Errorf("the bundle is larger than %d bytes", MaxBundle)
	}
	r := io.Reader(bytes.NewReader(data))
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		z, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("the bundle is not a valid gzip: %w", err)
		}
		r = z
	}
	raw, err := io.ReadAll(io.LimitReader(r, MaxUnpacked+1))
	if err != nil {
		return nil, fmt.Errorf("the bundle cannot be read: %w", err)
	}
	if len(raw) > MaxUnpacked {
		return nil, fmt.Errorf("the bundle unpacks to more than %d bytes", MaxUnpacked)
	}
	out := map[string]string{}
	if len(raw) >= 512 && bytes.Equal(raw[257:262], []byte("ustar")) {
		tr := tar.NewReader(bytes.NewReader(raw))
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("the bundle is not a valid tar: %w", err)
			}
			if h.Typeflag != tar.TypeReg {
				continue
			}
			name := h.Name[strings.LastIndex(h.Name, "/")+1:]
			if !strings.HasPrefix(name, "dmesg-") && name != "failsafe" && name != "pending" {
				continue
			}
			if len(out) == maxRecords {
				return nil, fmt.Errorf("more than %d records", maxRecords)
			}
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			out[name] = string(b)
		}
	} else if printable(raw) {
		out["dmesg-0"] = string(raw)
	} else {
		return nil, errors.New("the bundle is not the crash log the WebUI downloads (crashlog_*.tar.gz)")
	}
	return out, nil
}

func printable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	bad := 0
	for _, c := range b {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			bad++
		}
	}
	return bad*100 < len(b)
}

// Parse reads the records a bundle unpacked to.
func Parse(files map[string]string) (*Crash, error) {
	var names []string
	for n := range files {
		if strings.HasPrefix(n, "dmesg-") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	c := &Crash{Records: len(names), Anomalies: map[string]int{}}
	if f, ok := files["failsafe"]; ok && strings.Contains(f, "bootlimit") {
		c.Failsafe = true
	}
	if len(names) == 0 && !c.Failsafe {
		return nil, ErrNotACrash
	}

	sum := sha256.New()
	var texts []string
	seen := map[string]bool{}
	var traces []*Trace
	panicked, panicMsg := false, ""
	for _, n := range names {
		body := files[n]
		texts = append(texts, "==> "+n+" <==\n"+body)
		sum.Write([]byte(body))
		sum.Write([]byte{0})
		lines := strings.Split(strings.ReplaceAll(body, "\r", ""), "\n")
		if len(lines) > 0 && strings.HasPrefix(lines[0], "Panic#") {
			panicked = true
		}
		c.context(lines)
		ts, msg := traces1(lines)
		if msg != "" {
			panicked, panicMsg = true, msg
		}
		for _, t := range ts {
			k := t.Kind + t.end + t.Signature()
			if seen[k] {
				continue
			}
			seen[k] = true
			traces = append(traces, t)
		}
		if c.Leadup == nil {
			c.Leadup, c.Uptime = leadup(lines)
		}
	}
	c.Text = strings.Join(texts, "\n")
	if c.Failsafe {
		sum.Write([]byte("failsafe\x00" + files["failsafe"]))
	}
	c.ContentSum = hex.EncodeToString(sum.Sum(nil))

	for _, t := range traces {
		if t.Kind == KindWarning {
			c.Before = append(c.Before, t)
			continue
		}
		if c.Fatal == nil || Worse(t.Kind, c.Fatal.Kind) {
			c.Fatal = t
		}
	}
	switch {
	case c.Fatal != nil:
		c.Kind = c.Fatal.Kind
		if panicked {
			c.Kind = KindPanic
		}
		if strings.Contains(panicMsg, "in interrupt") {
			c.Fatal.InIRQ = true
		}
	case panicked:
		c.Fatal = &Trace{Kind: KindPanic, Reason: "Kernel panic: " + panicMsg}
		c.Kind = KindPanic
	case c.Failsafe:
		c.Fatal = &Trace{Kind: KindBootloop, Reason: "Boot loop: the watchdog's boot limit ran out"}
		c.Kind = KindBootloop
	case len(c.Before) > 0:
		// Only warnings: the record was written for something else (a
		// reboot that dumped the log); the last warning is what there is.
		c.Fatal = c.Before[len(c.Before)-1]
		c.Before = c.Before[:len(c.Before)-1]
		c.Kind = KindWarning
	default:
		return nil, errors.New("the records hold no oops, BUG, WARNING or panic")
	}
	for _, f := range c.Fatal.Frames {
		if strings.Contains(f.Fn, "sysrq") {
			c.SelfInflicted = true
		}
	}
	if strings.Contains(c.Text, "SysRq : Trigger a crash") || strings.Contains(c.Text, "sysrq: Trigger a crash") {
		c.SelfInflicted = true
	}
	for k, v := range c.Anomalies {
		if v == 0 {
			delete(c.Anomalies, k)
		}
	}
	return c, nil
}

// A printk line, its <level> prefix and [ timestamp] dropped.
var linePrefix = regexp.MustCompile(`^(?:<\d>)?(?:\[\s*\d+\.\d+\]\s?)?`)

func clean(l string) string { return linePrefix.ReplaceAllString(l, "") }

var (
	bannerRe  = regexp.MustCompile(`^Linux version (\S+) \((.*?)\) \((.*?)\) (#\d+.*)$`)
	sensorRe  = regexp.MustCompile(`sensor=([\w.-]+), chip=([\w.-]+), board=([\w.-]+?)=*$`)
	buildTime = regexp.MustCompile(`\w{3} \w{3} +\d+ \d\d:\d\d:\d\d \w+ \d{4}$`)
)

// anomalies are lines worth counting before a crash: a bus that stopped
// answering, memory running out, a CPU stuck.
var anomalies = []struct {
	name string
	re   *regexp.Regexp
}{
	{"i2c_error", regexp.MustCompile(`(?i)i2c.*(abort|timeout|timed out|error|nack|no ack)`)},
	{"spi_error", regexp.MustCompile(`(?i)\bspi\b.*(timeout|error)`)},
	{"mmz_alloc_failed", regexp.MustCompile(`(?i)(mmz|mmb|cma).*(fail|no memory|not enough)`)},
	{"oom", regexp.MustCompile(`(?i)out of memory|oom-killer|page allocation failure`)},
	{"soft_lockup", regexp.MustCompile(`(?i)soft lockup|hung_task|rcu.*stall`)},
	{"mtd_error", regexp.MustCompile(`(?i)(ubi|ubifs|jffs2|mtd|squashfs).*(error|err\b|corrupt)`)},
	{"ethernet_down", regexp.MustCompile(`(?i)eth\d.*link is down`)},
}

func (c *Crash) context(lines []string) {
	counts := map[string]int{}
	for _, raw := range lines {
		l := clean(raw)
		if m := bannerRe.FindStringSubmatch(l); m != nil && c.Kernel == "" {
			c.Kernel, c.Toolchain, c.KernelBuild = m[1], m[3], m[4]
			if s := buildTime.FindString(m[4]); s != "" {
				if t, err := time.Parse("Mon Jan _2 15:04:05 MST 2006", s); err == nil {
					c.KernelBuilt = t.UTC()
				}
			}
		}
		if v, ok := strings.CutPrefix(l, "Kernel command line: "); ok && c.Cmdline == "" {
			c.Cmdline = v
		}
		if _, v, ok := strings.Cut(l, "Machine model: "); ok && c.Machine == "" {
			c.Machine = v
		}
		if m := sensorRe.FindStringSubmatch(l); m != nil && c.SoC == "" {
			c.Sensor, c.SoC, c.Board = m[1], m[2], m[3]
		}
		if v, ok := strings.CutPrefix(l, "Modules linked in: "); ok && c.Modules == nil {
			for _, m := range strings.Fields(v) {
				c.Modules = append(c.Modules, strings.TrimSuffix(m, "(O)"))
			}
		}
		for _, a := range anomalies {
			if a.re.MatchString(l) {
				counts[a.name]++
			}
		}
	}
	// Records of one crash overlap: the most any of them saw.
	for k, v := range counts {
		c.Anomalies[k] = max(c.Anomalies[k], v)
	}
}

// Backtrace lines, by architecture:
//
//	ARM:    [<c003c428>] (__wake_up_common) from [<c003c4c0>] (__wake_up+0x24/0x30)
//	        [<bf00e060>] (osal_wakeup [open_osal]) from [<bf05d8cc>] (RGN_PutRegion+0x2d0/0x31c [open_rgn])
//	MIPS:   [<80123456>] foo+0x10/0x20 [module]
//	arm64:  [<ffffff8008123456>] foo+0x10/0x20 [module]   or   foo+0x10/0x20 [module]
var (
	armFrame   = regexp.MustCompile(`^\[<[0-9a-f]+>\] \(([^ ()+]+)(?:\+0x[0-9a-f]+/0x[0-9a-f]+)?(?: \[([\w-]+)\])?\) from \[<[0-9a-f]+>\] \(([^ ()+]+)`)
	otherFrame = regexp.MustCompile(`^(?:\[<[0-9a-f]+>\] )?\s*([A-Za-z_][\w.$]*)\+0x[0-9a-f]+/0x[0-9a-f]+(?: \[([\w-]+)\])?\s*$`)
	pcRe       = regexp.MustCompile(`^(?:PC is at |pc : (?:\[<[0-9a-f]+>\] )?|epc\s*: [0-9a-f]+ )([A-Za-z_][\w.$]*)`)
	commRe     = regexp.MustCompile(`^CPU: \d+ PID: \d+ Comm: (\S+)`)
	faultRe    = regexp.MustCompile(`^(?:CPU \d+ )?Unable to handle kernel (NULL pointer dereference|paging request) at virtual address`)
	// ARM's "Internal error: Oops: 5 [#1] ARM", MIPS's "Oops[#1]:" -- never
	// pstore's own "Oops#1 Part1" record header.
	oopsRe  = regexp.MustCompile(`^(?:Internal error: )?Oops(?:\[#\d+\]|: )`)
	bugRe   = regexp.MustCompile(`^(?:kernel BUG at (\S+)!|BUG: (.+))$`)
	warnRe  = regexp.MustCompile(`^WARNING: (?:CPU: \d+ PID: \d+ )?at (\S+)`)
	endRe   = regexp.MustCompile(`^---\[ end trace ([0-9a-f]+) \]---`)
	panicRe = regexp.MustCompile(`^Kernel panic - not syncing: (.*)$`)
)

// glue: frames every crash has. They say how the kernel got to the report,
// never what went wrong.
var glue = map[string]bool{
	"dump_backtrace": true, "show_stack": true, "dump_stack": true, "__dump_stack": true,
	"__warn": true, "warn_slowpath_fmt": true, "warn_slowpath_null": true, "warn_slowpath_common": true,
	"panic": true, "die": true, "__die": true, "oops_end": true, "bug_handler": true,
	"__do_kernel_fault": true, "do_page_fault": true, "do_translation_fault": true, "do_DataAbort": true,
	"do_PrefetchAbort": true, "do_undefinstr": true, "__dabt_svc": true, "__dabt_usr": true, "__pabt_usr": true,
	"__und_svc": true, "__irq_svc": true, "__irq_usr": true, "gic_handle_irq": true, "__handle_domain_irq": true,
	"generic_handle_irq": true, "handle_fasteoi_irq": true, "handle_level_irq": true, "handle_edge_irq": true,
	"handle_irq_event": true, "handle_irq_event_percpu": true, "__handle_irq_event_percpu": true,
	"irq_exit": true, "__irq_exit_rcu": true, "do_IRQ": true, "plat_irq_dispatch": true, "ret_from_irq": true,
	"ret_fast_syscall": true, "ret_from_fork": true, "ret_from_exception": true, "syscall_common": true,
	"vfs_ioctl": true, "do_vfs_ioctl": true, "SyS_ioctl": true, "sys_ioctl": true, "ksys_ioctl": true,
	"__arm64_sys_ioctl": true, "__se_sys_ioctl": true, "el0_svc_naked": true, "el0_svc": true, "el1_irq": true,
	"el0_irq": true, "el1_da": true, "el1_sync": true, "osal_unlocked_ioctl": true, "osal_ioctl": true,
	"__do_softirq": true,
}

// irqEntry: the frame where the interrupted code's frames begin.
var irqEntry = map[string]bool{"__irq_svc": true, "__irq_usr": true, "el1_irq": true, "el0_irq": true, "ret_from_irq": true}

// traces1 reads the traces in one record, and the panic message if it has one.
func traces1(lines []string) ([]*Trace, string) {
	var out []*Trace
	var cur *Trace
	panicMsg := ""
	afterIRQ := false
	start := func(kind, reason string) {
		if cur != nil && len(cur.Frames) == 0 && cur.Kind == KindOops && kind == KindOops {
			return // "Unable to handle..." then "Internal error: Oops": one oops
		}
		if cur != nil {
			out = append(out, cur)
		}
		cur, afterIRQ = &Trace{Kind: kind, Reason: reason}, false
	}
	for _, raw := range lines {
		l := clean(raw)
		switch {
		case faultRe.MatchString(l):
			m := faultRe.FindStringSubmatch(l)
			start(KindOops, strings.ToUpper(m[1][:1])+m[1][1:])
			continue
		case oopsRe.MatchString(l):
			if cur == nil || cur.Kind != KindOops || len(cur.Frames) > 0 {
				start(KindOops, "Oops")
			}
			continue
		case bugRe.MatchString(l):
			m := bugRe.FindStringSubmatch(l)
			if m[1] != "" {
				start(KindBug, "BUG at "+m[1])
			} else {
				start(KindBug, "BUG: "+strings.TrimSpace(m[2]))
			}
			continue
		case warnRe.MatchString(l):
			start(KindWarning, "WARNING at "+warnRe.FindStringSubmatch(l)[1])
			continue
		case panicRe.MatchString(l):
			panicMsg = strings.TrimSpace(panicRe.FindStringSubmatch(l)[1])
			if cur == nil && !fatalIn(out) {
				// A panic of its own (init killed, a watchdog): its
				// backtrace follows.
				start(KindPanic, "Kernel panic: "+panicMsg)
			}
			continue
		}
		if cur == nil {
			continue
		}
		if m := endRe.FindStringSubmatch(l); m != nil {
			cur.end = m[1]
			out = append(out, cur)
			cur = nil
			continue
		}
		if m := commRe.FindStringSubmatch(l); m != nil && cur.Comm == "" {
			cur.Comm = m[1]
			continue
		}
		if m := pcRe.FindStringSubmatch(l); m != nil && cur.PC == "" {
			cur.PC = m[1]
			continue
		}
		var f Frame
		var next string
		if m := armFrame.FindStringSubmatch(l); m != nil {
			f, next = Frame{Fn: m[1], Module: m[2]}, m[3]
		} else if m := otherFrame.FindStringSubmatch(l); m != nil {
			f = Frame{Fn: m[1], Module: m[2]}
		} else {
			continue
		}
		if irqEntry[f.Fn] || irqEntry[next] {
			cur.InIRQ = true
		}
		wasAfter := afterIRQ
		if irqEntry[f.Fn] || irqEntry[next] {
			afterIRQ = true
		}
		if irqEntry[f.Fn] || glue[f.Fn] {
			continue
		}
		if wasAfter {
			if len(cur.Interrupted) < sigFrames {
				cur.Interrupted = append(cur.Interrupted, f)
			}
		} else {
			cur.Frames = append(cur.Frames, f)
		}
	}
	if cur != nil {
		out = append(out, cur)
	}
	return out, panicMsg
}

func fatalIn(ts []*Trace) bool {
	for _, t := range ts {
		if t.Kind != KindWarning {
			return true
		}
	}
	return false
}

// leadup is the last lines before the first oops, BUG or panic -- what the
// kernel said just before -- and the uptime it happened at, when printk
// prints timestamps.
func leadup(lines []string) ([]string, float64) {
	for i, raw := range lines {
		l := clean(raw)
		if faultRe.MatchString(l) || oopsRe.MatchString(l) || bugRe.MatchString(l) || panicRe.MatchString(l) {
			var out []string
			for _, p := range lines[max(0, i-8):i] {
				p = strings.TrimSpace(clean(p))
				if p != "" && !strings.HasPrefix(p, "[<") && !registers.MatchString(p) {
					out = append(out, p)
				}
			}
			up, _ := uptime(raw)
			return out, up
		}
	}
	return nil, 0
}

var (
	registers = regexp.MustCompile(`^(r\d+|[0-9a-f]{4}):`)
	stamp     = regexp.MustCompile(`^(?:<\d>)?\[\s*(\d+\.\d+)\]`)
)

// uptime is a printk timestamp's seconds.
func uptime(l string) (float64, bool) {
	m := stamp.FindStringSubmatch(l)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return v, err == nil
}
