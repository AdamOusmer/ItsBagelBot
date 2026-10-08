// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkeytest

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func Simple(s string) Reply { return Reply("+" + s + "\r\n") }
func Int(v int64) Reply     { return Reply(":" + strconv.FormatInt(v, 10) + "\r\n") }
func Bulk(s string) Reply   { return Reply("$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n") }
func Nil() Reply            { return Reply("$-1\r\n") }
func Err(s string) Reply    { return Reply("-ERR " + s + "\r\n") }

func Flat(items []string) Reply {
	replies := make([]Reply, len(items))
	for i, item := range items {
		replies[i] = Bulk(item)
	}
	return Array(replies)
}

func Array(items []Reply) Reply {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(items))
	for _, item := range items {
		b.Write(item)
	}
	return Reply(b.String())
}

func readArray(r *bufio.Reader) (Args, error) {
	n, err := readCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make(Args, 0, n)
	for range n {
		size, err := readCount(r, '$')
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func readCount(r *bufio.Reader, prefix byte) (int, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" || line[0] != prefix {
		return 0, fmt.Errorf("expected %q-prefixed header, got %q", prefix, line)
	}
	return strconv.Atoi(line[1:])
}
