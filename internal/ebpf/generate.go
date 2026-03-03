package ebpf

//go:generate go tool bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64 querytap ../../bpf/querytap.c -- -I../../bpf
