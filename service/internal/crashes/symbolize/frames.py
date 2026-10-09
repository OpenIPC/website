# Run inside gdb by the symbolize package: the backtrace of the core loaded,
# and what a list of addresses are, as one JSON line each. gdb's own `bt` is
# for people; this is for a parser. Frame arguments are never read: they are
# majestic's memory, and the stack slice is enough to unwind.
import gdb
import json


def _where(pc, frame=None):
    out = {"pc": int(pc), "fn": "", "file": "", "line": 0}
    try:
        block = frame.block() if frame is not None else gdb.block_for_pc(int(pc))
        while block is not None and block.function is None:
            block = block.superblock
        if block is not None and block.function is not None:
            out["fn"] = block.function.name
    except RuntimeError:
        pass
    if not out["fn"] and frame is not None and frame.name():
        out["fn"] = frame.name()
    if not out["fn"]:
        sym = gdb.execute("info symbol 0x%x" % int(pc), to_string=True).strip()
        if sym and not sym.startswith("No symbol"):
            out["fn"] = sym.split(" ")[0]
    sal = frame.find_sal() if frame is not None else gdb.find_pc_line(int(pc))
    if sal is not None and sal.symtab is not None:
        out["file"] = sal.symtab.filename
        out["line"] = sal.line
    return out


def unwind(limit=64):
    frames = []
    stop = ""
    f = gdb.newest_frame()
    while f is not None and len(frames) < limit:
        w = _where(f.pc(), f)
        try:
            w["sp"] = int(f.read_register("sp"))
        except (gdb.error, ValueError):
            w["sp"] = 0
        w["inline"] = f.type() == gdb.INLINE_FRAME
        frames.append(w)
        try:
            older = f.older()
        except gdb.error as e:
            stop = str(e)
            break
        if older is None:
            stop = gdb.frame_stop_reason_string(f.unwind_stop_reason())
        f = older
    print("@@FRAMES " + json.dumps({"frames": frames, "stop": stop}))


def resolve(addrs):
    print("@@RESOLVED " + json.dumps([_where(a) for a in addrs]))
