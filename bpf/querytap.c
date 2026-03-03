//go:build ignore

// QueryTap BPF program — uprobe/uretprobe on mysqld dispatch_command.
//
// Uses self-contained type definitions (no system vmlinux.h dependency)
// so the program compiles on any Linux with clang, without needing
// kernel headers or bpftool-generated vmlinux.h.

#include "vmlinux_types.h"

// BPF helper definitions — inlined to avoid system header conflicts.
// These are stable kernel ABI.

static void *(*bpf_map_lookup_elem)(void *map, const void *key) = (void *)1;
static long (*bpf_map_update_elem)(void *map, const void *key, const void *value, __u64 flags) = (void *)2;
static long (*bpf_map_delete_elem)(void *map, const void *key) = (void *)3;
static __u64 (*bpf_ktime_get_ns)(void) = (void *)5;
static __u64 (*bpf_get_current_pid_tgid)(void) = (void *)14;
static long (*bpf_probe_read_user)(void *dst, __u32 size, const void *unsafe_ptr) = (void *)112;
static long (*bpf_probe_read_user_str)(void *dst, __u32 size, const void *unsafe_ptr) = (void *)114;
static void *(*bpf_ringbuf_reserve)(void *ringbuf, __u64 size, __u64 flags) = (void *)131;
static void (*bpf_ringbuf_submit)(void *data, __u64 flags) = (void *)132;
static long (*bpf_ringbuf_output)(void *ringbuf, void *data, __u64 size, __u64 flags) = (void *)130;

// SEC and map macros
#define SEC(name) __attribute__((section(name), used))
#define __uint(name, val) int (*name)[val]
#define __type(name, val) typeof(val) *name

// PT_REGS parameter access (x86_64)
#if defined(__TARGET_ARCH_x86) || defined(__x86_64__)
#define PT_REGS_PARM1(x) ((x)->di)
#define PT_REGS_PARM2(x) ((x)->si)
#define PT_REGS_PARM3(x) ((x)->dx)
#elif defined(__TARGET_ARCH_arm64) || defined(__aarch64__)
#define PT_REGS_PARM1(x) ((x)->regs[0])
#define PT_REGS_PARM2(x) ((x)->regs[1])
#define PT_REGS_PARM3(x) ((x)->regs[2])
#endif

#define MAX_QUERY_LEN 4096
#define EVENT_TYPE_QUERY 1
#define EVENT_TYPE_LATENCY 2
#define COM_QUERY 3

// Event structs — packed layout must match Go-side event.go exactly.
//
// Go QueryEvent wire format (DecodeEvent reads byte 0 as type, then payload):
//   [1 type][8 timestamp_ns][4 tid][1 command][2 query_len][4096 query] = 4112 bytes
//
// Go LatencyEvent wire format:
//   [1 type][8 timestamp_ns][4 tid][8 latency_ns] = 21 bytes

struct query_event {
    __u8  event_type;    // EVENT_TYPE_QUERY
    __u64 timestamp_ns;
    __u32 tid;
    __u8  command;
    __u16 query_len;
    char  query[MAX_QUERY_LEN];
} __attribute__((packed));

struct latency_event {
    __u8  event_type;    // EVENT_TYPE_LATENCY
    __u64 timestamp_ns;
    __u32 tid;
    __u64 latency_ns;
} __attribute__((packed));

// Maps

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 16 * 1024 * 1024); // 16 MB default
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u32);   // tid
    __type(value, __u64); // entry timestamp
} active_queries SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct query_event);
} query_buf SEC(".maps");

// Uprobe on dispatch_command entry.
//
// MySQL 8.x signature:
//   bool dispatch_command(THD *thd, const COM_DATA *com_data,
//                         enum enum_server_command command)
//
// arg0 = THD*, arg1 = COM_DATA*, arg2 = command enum
SEC("uprobe/dispatch_command")
int uprobe_dispatch_command(struct pt_regs *ctx) {
    __u32 tid = (__u32)bpf_get_current_pid_tgid();
    __u64 ts = bpf_ktime_get_ns();

    // Read command enum (arg2).
    __u8 command = (__u8)PT_REGS_PARM3(ctx);

    // Store entry timestamp for latency calculation on return.
    bpf_map_update_elem(&active_queries, &tid, &ts, BPF_ANY);

    // Only capture query text for COM_QUERY commands.
    if (command != COM_QUERY) {
        return 0;
    }

    // Use per-CPU scratch buffer to avoid blowing the 512-byte BPF stack.
    __u32 zero = 0;
    struct query_event *evt = bpf_map_lookup_elem(&query_buf, &zero);
    if (!evt) {
        return 0;
    }

    evt->event_type = EVENT_TYPE_QUERY;
    evt->timestamp_ns = ts;
    evt->tid = tid;
    evt->command = command;

    // COM_DATA is a union; for COM_QUERY the first field is a char* (query).
    const void *com_data_ptr = (const void *)PT_REGS_PARM2(ctx);
    const char *query_ptr = 0;

    bpf_probe_read_user(&query_ptr, sizeof(query_ptr), com_data_ptr);
    if (!query_ptr) {
        return 0;
    }

    // Read query text into scratch buffer.
    long ret = bpf_probe_read_user_str(evt->query, MAX_QUERY_LEN, query_ptr);
    if (ret > 0) {
        evt->query_len = (__u16)(ret - 1); // bpf_probe_read_user_str includes null terminator
    } else {
        evt->query_len = 0;
    }

    // Submit to ring buffer.
    bpf_ringbuf_output(&events, evt, sizeof(*evt), 0);

    return 0;
}

// Uretprobe on dispatch_command return — emits latency measurement.
SEC("uretprobe/dispatch_command")
int uretprobe_dispatch_command(struct pt_regs *ctx) {
    __u32 tid = (__u32)bpf_get_current_pid_tgid();
    __u64 ts = bpf_ktime_get_ns();

    // Look up the entry timestamp stored by the uprobe.
    __u64 *entry_ts = bpf_map_lookup_elem(&active_queries, &tid);
    if (!entry_ts) {
        return 0;
    }

    __u64 latency = ts - *entry_ts;

    // Clean up the hash entry.
    bpf_map_delete_elem(&active_queries, &tid);

    // Reserve directly in ring buffer (latency_event is small enough).
    struct latency_event *evt = bpf_ringbuf_reserve(&events, sizeof(struct latency_event), 0);
    if (!evt) {
        return 0;
    }

    evt->event_type = EVENT_TYPE_LATENCY;
    evt->timestamp_ns = ts;
    evt->tid = tid;
    evt->latency_ns = latency;

    bpf_ringbuf_submit(evt, 0);

    return 0;
}

char __license[] SEC("license") = "MIT";
