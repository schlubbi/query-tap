#ifndef __VMLINUX_TYPES_H__
#define __VMLINUX_TYPES_H__

typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;
typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;

typedef __u8 u8;
typedef __u16 u16;
typedef __u32 u32;
typedef __u64 u64;

// Minimal pt_regs — actual layout provided by bpf_tracing.h.
// PT_REGS_PARM macros are architecture-specific and supplied by the
// BPF helper headers, so we don't need a full struct definition here.

#endif /* __VMLINUX_TYPES_H__ */
