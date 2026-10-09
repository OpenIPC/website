/* A stand-in for majestic in the crashes package's tests: a PIE that dies of
 * SIGSEGV and writes the dump majestic writes (MJCD, docs in ../../dump.go),
 * so the symbolizer is tested against a real fault on a real camera without
 * majestic's own binary. gen.sh builds it and says how the fixtures were made.
 *
 *   toy own     -- a NULL store in this program: frames gdb can unwind
 *   toy lib     -- a NULL load inside libtoy.so, which has no unwind tables,
 *                  like the vendor libraries a camera's majestic loads
 *   toy libc    -- strlen(NULL): a fault inside the C library
 *   toy sent    -- raise(SIGSEGV): a signal sent, not a fault */
#define _GNU_SOURCE
#include <elf.h>
#include <fcntl.h>
#include <link.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/syscall.h>
#include <ucontext.h>
#include <unistd.h>

int toy_lib_fault(const char *s); /* libtoy.so */

#if defined(__arm__)
#define ARCH 1
#define SP(m) ((m)->arm_sp)
#elif defined(__mips__) && defined(__MIPSEL__)
#define ARCH 2
#define SP(m) ((m)->gregs[29])
#elif defined(__mips__)
#define ARCH 3
#define SP(m) ((m)->gregs[29])
#elif defined(__x86_64__)
#define ARCH 4
#define SP(m) ((m)->gregs[REG_RSP])
#elif defined(__aarch64__)
#define ARCH 5
#define SP(m) ((m)->sp)
#endif

static char dir[256] = ".";
static char mods[4096];
static size_t modslen;

static int out = -1;

static void put(const void *p, size_t n) {
    if (write(out, p, n) < 0) {
    }
}

static void section(const char *tag, const void *p, uint32_t n) {
    put(tag, 4);
    put(&n, 4);
    put(p, n);
}

/* A file copied into a section: its length is patched in afterwards. */
static void file_section(const char *tag, const char *path) {
    char buf[1024];
    uint32_t n = 0;
    put(tag, 4);
    off_t at = lseek(out, 0, SEEK_CUR);
    put(&n, 4);
    int fd = open(path, O_RDONLY);
    ssize_t r;
    while (fd >= 0 && (r = read(fd, buf, sizeof buf)) > 0) {
        put(buf, r);
        n += r;
    }
    if (fd >= 0)
        close(fd);
    off_t end = lseek(out, 0, SEEK_CUR);
    lseek(out, at, SEEK_SET);
    put(&n, 4);
    lseek(out, end, SEEK_SET);
}

static int module(struct dl_phdr_info *info, size_t size, void *arg) {
    (void)size;
    (void)arg;
    char id[41] = "-";
    for (int i = 0; i < info->dlpi_phnum; i++) {
        const ElfW(Phdr) *ph = &info->dlpi_phdr[i];
        if (ph->p_type != PT_NOTE)
            continue;
        const uint8_t *p = (const uint8_t *)(info->dlpi_addr + ph->p_vaddr);
        const uint8_t *end = p + ph->p_memsz;
        while (p + 12 <= end) {
            uint32_t nsz, dsz, type;
            memcpy(&nsz, p, 4);
            memcpy(&dsz, p + 4, 4);
            memcpy(&type, p + 8, 4);
            const uint8_t *desc = p + 12 + ((nsz + 3) & ~3u);
            if (type == NT_GNU_BUILD_ID && nsz == 4 && !memcmp(p + 12, "GNU", 4) && dsz <= 20) {
                for (uint32_t j = 0; j < dsz; j++)
                    sprintf(id + 2 * j, "%02x", desc[j]);
            }
            p = desc + ((dsz + 3) & ~3u);
        }
    }
    uintptr_t start = 0;
    for (int i = 0; i < info->dlpi_phnum; i++)
        if (info->dlpi_phdr[i].p_type == PT_LOAD) {
            start = info->dlpi_addr + info->dlpi_phdr[i].p_vaddr;
            break;
        }
    /* The first is the program (named "" by glibc, argv[0] by musl); a later
     * one without a name is the vdso. */
    static int seen;
    const char *name = info->dlpi_name;
    char self[256];
    if (!seen++) {
        ssize_t n = readlink("/proc/self/exe", self, sizeof self - 1);
        self[n > 0 ? n : 0] = '\0';
        name = self;
    } else if (!name || !*name) {
        name = "[vdso]";
    }
    modslen += snprintf(mods + modslen, sizeof mods - modslen, "%s 0x%lx 0x%lx %s\n", id,
                        (unsigned long)start, (unsigned long)info->dlpi_addr, name);
    return 0;
}

static void on_fault(int sig, siginfo_t *si, void *ctx) {
    ucontext_t *uc = ctx;
    char path[300];
    snprintf(path, sizeof path, "%s/majestic.dump", dir);
    out = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    const uint8_t head[8] = {'M', 'J', 'C', 'D', 1, 0, ARCH, 0};
    put(head, 8);

    char hdr[512];
    int n = snprintf(hdr, sizeof hdr,
                     "signal=%d\ncode=%d\naddr=0x%lx\npid=%d\ntid=%ld\nthread=toy\nwall=1791500000\n"
                     "uptime=100\nversion=toy+0000000, 2026-10-09 00:00\n",
                     sig, si->si_code, (unsigned long)si->si_addr, (int)getpid(), (long)syscall(SYS_gettid));
    if (si->si_code <= 0)
        n += snprintf(hdr + n, sizeof hdr - n, "sender=%d\n", (int)si->si_pid);
    section("HDR ", hdr, n);
    section("REGS", &uc->uc_mcontext, sizeof uc->uc_mcontext);

    /* The stack from sp to its mapping's end, at most 32 KiB: write() says
     * EFAULT where it stops. */
    uint64_t sp = (uintptr_t)SP(&uc->uc_mcontext);
    put("STAK", 4);
    off_t at = lseek(out, 0, SEEK_CUR);
    uint32_t len = 8;
    put(&len, 4);
    put(&sp, 8);
    for (uintptr_t p = sp; p < sp + 32768;) {
        uintptr_t next = (p | 4095) + 1;
        ssize_t w = write(out, (const void *)p, next - p);
        if (w <= 0)
            break;
        len += w;
        p += w;
    }
    off_t end = lseek(out, 0, SEEK_CUR);
    lseek(out, at, SEEK_SET);
    put(&len, 4);
    lseek(out, end, SEEK_SET);

    file_section("MAPS", "/proc/self/maps");
    file_section("AUXV", "/proc/self/auxv");
    section("MODS", mods, modslen);
    section("THRD", "1 toy\n", 6);
    section("END ", "", 0);
    close(out);
    signal(sig, SIG_DFL);
    raise(sig);
}

__attribute__((noinline)) static void store(volatile int *p, int v) { *p = v; }

__attribute__((noinline)) static int parse_level(const char *arg) {
    volatile int *slot = NULL;
    store(slot, (int)strlen(arg));
    return 0;
}

__attribute__((noinline)) static int name_length(const char *name) {
    return (int)strlen(name) + 1;
}

__attribute__((noinline)) static int through_library(const char *arg) {
    return toy_lib_fault(arg[0] == 'l' ? NULL : arg) + 1;
}

int main(int argc, char **argv) {
    if (argc > 2)
        snprintf(dir, sizeof dir, "%s", argv[2]);
    dl_iterate_phdr(module, NULL);
    static char alt[65536];
    stack_t ss = {.ss_sp = alt, .ss_size = sizeof alt};
    sigaltstack(&ss, NULL);
    struct sigaction sa = {.sa_sigaction = on_fault, .sa_flags = SA_SIGINFO | SA_ONSTACK};
    sigaction(SIGSEGV, &sa, NULL);
    const char *mode = argc > 1 ? argv[1] : "own";
    if (!strcmp(mode, "sent"))
        raise(SIGSEGV);
    if (!strcmp(mode, "libc"))
        return name_length(argc > 3 ? argv[3] : NULL);
    if (!strcmp(mode, "lib"))
        return through_library(mode);
    return parse_level(mode);
}
