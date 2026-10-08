package crashes

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"strings"
	"testing"
)

func bundle(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func parseBundle(t *testing.T, data []byte) *Crash {
	t.Helper()
	files, err := Unpack(data)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(files)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// tgz packs records the way S98crashlog does.
func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	tw := tar.NewWriter(z)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o444, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	z.Close()
	return buf.Bytes()
}

// The lab's gk7205v300 + imx335: the OSD region code oopsed in the VENC
// interrupt while hi_isp_run was in the ISP driver, and the kernel panicked.
// pstore kept it twice (Oops#1, Panic#2); it is one crash.
func TestLabBundle(t *testing.T) {
	c := parseBundle(t, bundle(t, "gk7205v300-imx335-rgn.tar.gz"))
	if c.Records != 2 || c.Kind != KindPanic {
		t.Fatalf("records %d kind %q, want 2 records, a panic", c.Records, c.Kind)
	}
	f := c.Fatal
	if f.Kind != KindOops || f.Reason != "NULL pointer dereference" || f.Comm != "hi_isp_run" || f.PC != "__wake_up_common" || !f.InIRQ {
		t.Fatalf("fatal %+v", f)
	}
	var got []string
	for _, fr := range f.Frames[:5] {
		got = append(got, fr.String())
	}
	want := "__wake_up_common|__wake_up|osal_wakeup [open_osal]|RGN_PutRegion [open_rgn]|VencPutOsd [open_venc]"
	if strings.Join(got, "|") != want {
		t.Fatalf("frames\n %s\nwant\n %s", strings.Join(got, "|"), want)
	}
	if len(f.Interrupted) == 0 || f.Interrupted[0].Fn != "osal_spin_unlock_irqrestore" || f.Interrupted[1].Fn != "ISP_DRV_BeBufCtl" {
		t.Fatalf("interrupted %v", f.Interrupted)
	}
	if f.Title() != "NULL pointer dereference in __wake_up_common ← RGN_PutRegion [open_rgn]" {
		t.Fatalf("title %q", f.Title())
	}
	if len(c.Before) != 1 || c.Before[0].Reason != "WARNING at mm/page_alloc.c:7373" || c.Before[0].Comm != "majestic" ||
		c.Before[0].Frames[0].Fn != "free_contig_range" {
		t.Fatalf("before %+v", c.Before)
	}
	if c.SoC != "gk7205v300" || c.Sensor != "imx335" || c.Board != "demo" || c.Kernel != "4.9.37" ||
		c.KernelBuilt.Format("2006-01-02T15:04:05") != "2026-09-24T17:57:19" || !strings.HasPrefix(c.Cmdline, "mem=128M") ||
		c.Machine != "Goke GK7205V300 DEMO Board" || len(c.Modules) == 0 || c.Modules[0] != "open_cipher" {
		t.Fatalf("context %+v", c)
	}
	if c.Anomalies["i2c_error"] != 16 {
		t.Fatalf("anomalies %v, want 16 i2c errors (one record's)", c.Anomalies)
	}
	if len(c.Leadup) == 0 || !strings.Contains(c.Leadup[len(c.Leadup)-1], "i2c") {
		t.Fatalf("leadup %q", c.Leadup)
	}
	if c.SelfInflicted {
		t.Fatal("a real crash flagged self-inflicted")
	}
	// The same records packed again are the same crash.
	files, _ := Unpack(bundle(t, "gk7205v300-imx335-rgn.tar.gz"))
	if again := parseBundle(t, tgz(t, files)); again.ContentSum != c.ContentSum || again.Fatal.Signature() != f.Signature() {
		t.Fatal("repacked bundle is another crash")
	}
}

const sysrq = `Panic#1 Part1
<6>SysRq : Trigger a crash
<1>Unable to handle kernel NULL pointer dereference at virtual address 00000000
<0>Internal error: Oops: 817 [#1] ARM
<0>CPU: 0 PID: 1203 Comm: sh Tainted: G           O    4.9.37 #1
<0>PC is at sysrq_handle_crash+0x24/0x2c
<0>Backtrace:
<0>[<c0234a10>] (sysrq_handle_crash) from [<c0234e8c>] (__handle_sysrq+0xa4/0x154)
<0>[<c0234de8>] (__handle_sysrq) from [<c0235344>] (write_sysrq_trigger+0x4c/0x5c)
<0>[<c02352f8>] (write_sysrq_trigger) from [<c010f2a8>] (proc_reg_write+0x6c/0x90)
<4>---[ end trace 0000000000000001 ]---
<0>Kernel panic - not syncing: Fatal exception
`

func TestSysrqIsSelfInflicted(t *testing.T) {
	c := parseBundle(t, tgz(t, map[string]string{"dmesg-ramoops-0": sysrq}))
	if !c.SelfInflicted || c.Kind != KindPanic || c.Fatal.Frames[0].Fn != "sysrq_handle_crash" {
		t.Fatalf("%+v", c)
	}
}

// MIPS (Ingenic) prints "Call Trace:" with one function per line.
const mips = `<1>CPU 0 Unable to handle kernel paging request at virtual address 00000010, epc == 80123456, ra == 80123400
<4>Oops[#1]:
<4>CPU: 0 PID: 712 Comm: majestic Tainted: G           O    3.10.14 #1
<4>epc   : 80123456 tx_isp_frame_done+0x30/0x90 [tx-isp-t31]
<4>Call Trace:
<4>[<80123456>] tx_isp_frame_done+0x30/0x90 [tx-isp-t31]
<4>[<80123500>] isp_irq_handle+0x120/0x200 [tx-isp-t31]
<4>[<8004a000>] handle_irq_event_percpu+0x50/0x1a0
<4>[<8004d000>] handle_level_irq+0xa0/0x120
<4>[<80001000>] do_IRQ+0x20/0x30
<4>---[ end trace 1111111111111111 ]---
<0>Kernel panic - not syncing: Fatal exception in interrupt
`

func TestMIPS(t *testing.T) {
	// The MIPS oops line starts with "CPU 0 Unable to handle"; it is the
	// "Oops[#1]" that opens it.
	c := parseBundle(t, []byte(strings.Replace(mips, "Oops[#1]:", "Internal error: Oops[#1]:", 1)))
	if c.Kind != KindPanic || c.Fatal.Comm != "majestic" || c.Fatal.PC != "tx_isp_frame_done" || !c.Fatal.InIRQ {
		t.Fatalf("%+v", c.Fatal)
	}
	if len(c.Fatal.Frames) != 2 || c.Fatal.Frames[1].String() != "isp_irq_handle [tx-isp-t31]" {
		t.Fatalf("frames %v", c.Fatal.Frames)
	}
}

func TestFailsafeOnly(t *testing.T) {
	c := parseBundle(t, tgz(t, map[string]string{"failsafe": "reason=bootlimit\nutc=0\n"}))
	if c.Kind != KindBootloop || c.Fatal.Signature() == "" {
		t.Fatalf("%+v", c)
	}
}

func TestNotACrash(t *testing.T) {
	if _, err := Unpack(append([]byte{0x1f, 0x8b}, make([]byte, 50)...)); err == nil {
		t.Fatal("a broken gzip unpacked")
	}
	files, err := Unpack(tgz(t, map[string]string{"pending": "utc=1\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(files); err != ErrNotACrash {
		t.Fatalf("err %v", err)
	}
	if _, err := Parse(map[string]string{"dmesg-0": "<6>Booting Linux\n<6>all fine\n"}); err == nil {
		t.Fatal("a clean log is a crash")
	}
	if _, err := Unpack(make([]byte, MaxBundle+1)); err == nil {
		t.Fatal("an oversized bundle unpacked")
	}
}
